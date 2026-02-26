// Package syncer provides background reconciliation from SMD into local power mapping data.
package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	stdsync "sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-power/internal/model"
	"git.cscs.ch/openchami/chamicore-power/internal/store"
	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
	smdtypes "git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

const (
	defaultSyncInterval         = 5 * time.Minute
	defaultStartupRetryInterval = 1 * time.Second
	maxSyncPageSize             = 10000
)

// SMDClient describes the SMD calls used by the topology sync loop.
type SMDClient interface {
	ListComponents(ctx context.Context, opts smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error)
	ListEthernetInterfaces(ctx context.Context, opts smdclient.InterfaceListOptions) (*httputil.ResourceList[smdtypes.EthernetInterface], error)
}

// Config contains sync-loop settings.
type Config struct {
	Interval             time.Duration
	StartupRetryInterval time.Duration
	SyncOnStartup        bool
	DefaultCredentialID  string
}

// Status captures current and last-run mapping sync state.
type Status struct {
	Ready             bool                     `json:"ready"`
	InProgress        bool                     `json:"in_progress"`
	LastAttemptAt     *time.Time               `json:"last_attempt_at,omitempty"`
	LastSyncAt        *time.Time               `json:"last_sync_at,omitempty"`
	LastComponentETag string                   `json:"last_component_etag,omitempty"`
	LastInterfaceETag string                   `json:"last_interface_etag,omitempty"`
	LastError         string                   `json:"last_error,omitempty"`
	NotModified       bool                     `json:"not_modified"`
	LastCounts        model.MappingApplyCounts `json:"last_counts"`
	SuccessfulRuns    int64                    `json:"successful_runs"`
	FailedRuns        int64                    `json:"failed_runs"`
}

// Syncer runs periodic and on-demand reconciliation from SMD.
type Syncer struct {
	store store.Store
	smd   SMDClient
	log   zerolog.Logger

	interval            time.Duration
	startupRetry        time.Duration
	syncOnStartup       bool
	defaultCredentialID string
	forceSyncCh         chan chan error

	runMu   stdsync.Mutex
	stateMu stdsync.RWMutex
	status  Status

	lastComponentETag string
	lastInterfaceETag string

	ready atomic.Bool
}

// New creates a new mapping syncer.
func New(st store.Store, smd SMDClient, cfg Config, logger zerolog.Logger) *Syncer {
	interval := cfg.Interval
	if interval <= 0 {
		interval = defaultSyncInterval
	}
	startupRetry := cfg.StartupRetryInterval
	if startupRetry <= 0 {
		startupRetry = defaultStartupRetryInterval
	}

	return &Syncer{
		store:               st,
		smd:                 smd,
		log:                 logger,
		interval:            interval,
		startupRetry:        startupRetry,
		syncOnStartup:       cfg.SyncOnStartup,
		defaultCredentialID: strings.TrimSpace(cfg.DefaultCredentialID),
		forceSyncCh:         make(chan chan error),
	}
}

// Run starts the sync loop and blocks until ctx is canceled.
func (s *Syncer) Run(ctx context.Context) {
	if s.syncOnStartup {
		if err := s.SyncOnce(ctx); err != nil {
			s.log.Error().Err(err).Msg("initial mapping sync failed")
		}
	}

	tickerInterval := s.interval
	if s.syncOnStartup && !s.IsReady() {
		tickerInterval = s.startupRetry
	}

	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.SyncOnce(ctx); err != nil {
				s.log.Error().Err(err).Msg("periodic mapping sync failed")
			}
			tickerInterval = s.adjustTickerInterval(ticker, tickerInterval)
		case resultCh := <-s.forceSyncCh:
			resultCh <- s.SyncOnce(ctx)
			tickerInterval = s.adjustTickerInterval(ticker, tickerInterval)
		}
	}
}

// Trigger requests an immediate sync and waits for the result.
func (s *Syncer) Trigger(ctx context.Context) error {
	resultCh := make(chan error, 1)

	select {
	case s.forceSyncCh <- resultCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-resultCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsReady reports whether at least one successful sync has completed.
func (s *Syncer) IsReady() bool {
	return s.ready.Load()
}

// Status returns the latest sync status snapshot.
func (s *Syncer) Status() Status {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	statusCopy := s.status
	statusCopy.Ready = s.ready.Load()
	statusCopy.LastAttemptAt = cloneTimePtr(s.status.LastAttemptAt)
	statusCopy.LastSyncAt = cloneTimePtr(s.status.LastSyncAt)
	return statusCopy
}

// SyncOnce executes one mapping reconciliation cycle.
func (s *Syncer) SyncOnce(ctx context.Context) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	startedAt := time.Now().UTC()
	s.updateStatus(func(st *Status) {
		st.InProgress = true
		st.LastAttemptAt = &startedAt
		st.LastError = ""
		st.NotModified = false
	})
	defer s.updateStatus(func(st *Status) {
		st.InProgress = false
	})

	components, err := s.smd.ListComponents(ctx, smdclient.ComponentListOptions{
		Fields:      "id,type,parentId",
		Limit:       maxSyncPageSize,
		Offset:      0,
		IfNoneMatch: s.getLastComponentETag(),
	})
	if err != nil {
		s.markFailure(err)
		return fmt.Errorf("listing components from SMD: %w", err)
	}

	interfaces, err := s.smd.ListEthernetInterfaces(ctx, smdclient.InterfaceListOptions{
		Limit:  maxSyncPageSize,
		Offset: 0,
	})
	if err != nil {
		s.markFailure(err)
		return fmt.Errorf("listing ethernet interfaces from SMD: %w", err)
	}

	interfaceETag, err := computeResourceListETag(interfaces)
	if err != nil {
		s.markFailure(err)
		return fmt.Errorf("computing interface etag: %w", err)
	}

	if components == nil && interfaceETag == s.getLastInterfaceETag() {
		completedAt := time.Now().UTC()
		s.updateStatus(func(st *Status) {
			st.LastSyncAt = &completedAt
			st.NotModified = true
			st.LastCounts = model.MappingApplyCounts{}
			st.SuccessfulRuns++
		})
		return nil
	}

	if components == nil {
		components, err = s.smd.ListComponents(ctx, smdclient.ComponentListOptions{
			Fields: "id,type,parentId",
			Limit:  maxSyncPageSize,
			Offset: 0,
		})
		if err != nil {
			s.markFailure(err)
			return fmt.Errorf("refreshing components from SMD: %w", err)
		}
		if components == nil {
			s.markFailure(fmt.Errorf("component refresh unexpectedly returned nil"))
			return fmt.Errorf("refreshing components from SMD: empty response")
		}
	}

	componentETag, err := computeResourceListETag(components)
	if err != nil {
		s.markFailure(err)
		return fmt.Errorf("computing component etag: %w", err)
	}

	existingEndpoints, err := s.store.ListBMCEndpoints(ctx)
	if err != nil {
		s.markFailure(err)
		return fmt.Errorf("listing existing BMC endpoint mappings: %w", err)
	}

	desiredEndpoints, desiredLinks := buildDesiredMappings(
		components,
		interfaces,
		existingEndpoints,
		s.defaultCredentialID,
	)

	syncedAt := time.Now().UTC()
	counts, err := s.store.ReplaceTopologyMappings(ctx, desiredEndpoints, desiredLinks, syncedAt)
	if err != nil {
		s.markFailure(err)
		return fmt.Errorf("reconciling topology mappings: %w", err)
	}

	s.setLastComponentETag(componentETag)
	s.setLastInterfaceETag(interfaceETag)
	s.ready.Store(true)

	completedAt := time.Now().UTC()
	s.updateStatus(func(st *Status) {
		st.LastSyncAt = &completedAt
		st.LastComponentETag = componentETag
		st.LastInterfaceETag = interfaceETag
		st.LastCounts = counts
		st.NotModified = false
		st.SuccessfulRuns++
		st.LastError = ""
	})

	return nil
}

func buildDesiredMappings(
	components *httputil.ResourceList[smdtypes.Component],
	interfaces *httputil.ResourceList[smdtypes.EthernetInterface],
	existingEndpoints []model.BMCEndpoint,
	defaultCredentialID string,
) ([]model.BMCEndpoint, []model.NodeBMCLink) {
	bmcIDs := make(map[string]struct{})
	nodeToBMC := make(map[string]string)

	for _, item := range components.Items {
		componentID := strings.TrimSpace(item.Spec.ID)
		if componentID == "" {
			continue
		}

		parentID := ""
		if item.Spec.ParentID != nil {
			parentID = strings.TrimSpace(*item.Spec.ParentID)
		}

		componentType := strings.TrimSpace(item.Spec.Type)

		if isBMCType(componentType) {
			bmcIDs[componentID] = struct{}{}
		}
		if isNodeType(componentType) && parentID != "" {
			nodeToBMC[componentID] = parentID
			bmcIDs[parentID] = struct{}{}
		}
	}

	bmcEndpointFromSMD := make(map[string]string)
	for _, item := range interfaces.Items {
		componentID := strings.TrimSpace(item.Spec.ComponentID)
		if componentID == "" {
			continue
		}
		if _, tracked := bmcIDs[componentID]; !tracked {
			continue
		}
		if _, exists := bmcEndpointFromSMD[componentID]; exists {
			continue
		}

		if endpoint := endpointFromIPAddrs(item.Spec.IPAddrs); endpoint != "" {
			bmcEndpointFromSMD[componentID] = endpoint
		}
	}

	existingByBMC := make(map[string]model.BMCEndpoint, len(existingEndpoints))
	for _, endpoint := range existingEndpoints {
		existingByBMC[strings.TrimSpace(endpoint.BMCID)] = endpoint
	}

	bmcIDList := make([]string, 0, len(bmcIDs))
	for bmcID := range bmcIDs {
		bmcIDList = append(bmcIDList, bmcID)
	}
	sort.Strings(bmcIDList)

	endpoints := make([]model.BMCEndpoint, 0, len(bmcIDList))
	for _, bmcID := range bmcIDList {
		existing := existingByBMC[bmcID]
		endpoint := strings.TrimSpace(bmcEndpointFromSMD[bmcID])
		if endpoint == "" {
			endpoint = strings.TrimSpace(existing.Endpoint)
		}

		credentialID := strings.TrimSpace(existing.CredentialID)
		if credentialID == "" {
			credentialID = strings.TrimSpace(defaultCredentialID)
		}

		endpoints = append(endpoints, model.BMCEndpoint{
			BMCID:              bmcID,
			Endpoint:           endpoint,
			CredentialID:       credentialID,
			InsecureSkipVerify: existing.InsecureSkipVerify,
			Source:             "smd",
		})
	}

	nodeIDs := make([]string, 0, len(nodeToBMC))
	for nodeID := range nodeToBMC {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)

	links := make([]model.NodeBMCLink, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		links = append(links, model.NodeBMCLink{
			NodeID: nodeID,
			BMCID:  nodeToBMC[nodeID],
			Source: "smd",
		})
	}

	return endpoints, links
}

func endpointFromIPAddrs(ipAddrs json.RawMessage) string {
	addresses := parseIPAddrs(ipAddrs)
	for _, address := range addresses {
		if endpoint := normalizeEndpoint(address); endpoint != "" {
			return endpoint
		}
	}
	return ""
}

func parseIPAddrs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	addresses := make([]string, 0, 4)
	if err := json.Unmarshal(raw, &addresses); err == nil {
		return addresses
	}

	var generic []any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil
	}

	addresses = make([]string, 0, len(generic))
	for _, value := range generic {
		asString, ok := value.(string)
		if !ok {
			continue
		}
		addresses = append(addresses, asString)
	}
	return addresses
}

func normalizeEndpoint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	normalized := trimmed
	if !strings.Contains(normalized, "://") {
		normalized = "https://" + normalized
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return ""
	}
	if parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}

	return parsed.Scheme + "://" + parsed.Host
}

func isNodeType(componentType string) bool {
	return strings.EqualFold(strings.TrimSpace(componentType), "Node")
}

func isBMCType(componentType string) bool {
	return strings.EqualFold(strings.TrimSpace(componentType), "BMC")
}

func computeResourceListETag[T any](list *httputil.ResourceList[T]) (string, error) {
	if list == nil {
		return "", fmt.Errorf("resource list is nil")
	}
	encoded, err := json.Marshal(list)
	if err != nil {
		return "", fmt.Errorf("marshaling resource list: %w", err)
	}
	encoded = append(encoded, '\n')
	hash := sha256.Sum256(encoded)
	return fmt.Sprintf(`W/"%x"`, hash[:8]), nil
}

func cloneTimePtr(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

func (s *Syncer) markFailure(err error) {
	s.updateStatus(func(st *Status) {
		st.FailedRuns++
		st.LastError = err.Error()
		st.NotModified = false
		st.LastCounts = model.MappingApplyCounts{}
	})
}

func (s *Syncer) getLastComponentETag() string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.lastComponentETag
}

func (s *Syncer) setLastComponentETag(etag string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.lastComponentETag = etag
	s.status.LastComponentETag = etag
}

func (s *Syncer) getLastInterfaceETag() string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.lastInterfaceETag
}

func (s *Syncer) setLastInterfaceETag(etag string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.lastInterfaceETag = etag
	s.status.LastInterfaceETag = etag
}

func (s *Syncer) updateStatus(update func(*Status)) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	update(&s.status)
}

func (s *Syncer) adjustTickerInterval(ticker *time.Ticker, currentInterval time.Duration) time.Duration {
	desiredInterval := s.interval
	if s.syncOnStartup && !s.IsReady() {
		desiredInterval = s.startupRetry
	}
	if desiredInterval != currentInterval {
		ticker.Reset(desiredInterval)
		return desiredInterval
	}
	return currentInterval
}
