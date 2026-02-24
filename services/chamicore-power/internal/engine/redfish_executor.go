package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	sharedredfish "git.cscs.ch/openchami/chamicore-lib/redfish"
)

// CredentialResolver resolves Redfish credentials for one credential ID.
type CredentialResolver interface {
	Resolve(ctx context.Context, credentialID string) (sharedredfish.Credential, error)
}

// EmptyCredentialResolver resolves to empty credentials (unauthenticated Redfish).
type EmptyCredentialResolver struct{}

// Resolve returns empty Redfish credentials.
func (EmptyCredentialResolver) Resolve(ctx context.Context, credentialID string) (sharedredfish.Credential, error) {
	_ = ctx
	_ = credentialID
	return sharedredfish.Credential{}, nil
}

// RedfishAPI captures the Redfish calls used by the executor and verifier.
type RedfishAPI interface {
	ListSystemPaths(ctx context.Context, endpoint string, cred sharedredfish.Credential) ([]string, error)
	ResetSystem(ctx context.Context, endpoint, systemPath string, cred sharedredfish.Credential, operation sharedredfish.ResetOperation) error
	GetSystemPowerState(ctx context.Context, endpoint, systemPath string, cred sharedredfish.Credential) (string, error)
}

// SystemPathResolver caches endpoint/node -> system-path lookups.
type SystemPathResolver struct {
	cache sync.Map
}

// NewSystemPathResolver creates a system-path resolver.
func NewSystemPathResolver() *SystemPathResolver {
	return &SystemPathResolver{}
}

// Resolve discovers or returns cached Redfish system path for a node.
func (r *SystemPathResolver) Resolve(ctx context.Context, client RedfishAPI, endpoint, nodeID string, cred sharedredfish.Credential) (string, error) {
	normalizedEndpoint, err := sharedredfish.NormalizeEndpoint(endpoint)
	if err != nil {
		return "", err
	}

	key := cacheKey(normalizedEndpoint, nodeID)
	if cached, ok := r.cache.Load(key); ok {
		if path, ok := cached.(string); ok && strings.TrimSpace(path) != "" {
			return path, nil
		}
	}

	systemPaths, err := client.ListSystemPaths(ctx, normalizedEndpoint, cred)
	if err != nil {
		return "", err
	}
	if len(systemPaths) == 0 {
		return "", fmt.Errorf("endpoint %q has no Redfish systems", normalizedEndpoint)
	}

	chosen := selectSystemPath(systemPaths, nodeID)
	r.cache.Store(key, chosen)
	return chosen, nil
}

func cacheKey(endpoint, nodeID string) string {
	return strings.TrimSpace(endpoint) + "|" + strings.TrimSpace(nodeID)
}

func selectSystemPath(paths []string, nodeID string) string {
	normalizedNodeID := strings.TrimSpace(nodeID)
	if normalizedNodeID != "" {
		for _, path := range paths {
			trimmedPath := strings.TrimSpace(path)
			if trimmedPath == "" {
				continue
			}
			id := systemIDFromPath(trimmedPath)
			if strings.EqualFold(id, normalizedNodeID) {
				return trimmedPath
			}
		}
	}

	copied := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmedPath := strings.TrimSpace(path)
		if trimmedPath != "" {
			copied = append(copied, trimmedPath)
		}
	}
	if len(copied) == 0 {
		return ""
	}
	if len(copied) == 1 {
		return copied[0]
	}
	sort.Strings(copied)
	return copied[0]
}

func systemIDFromPath(systemPath string) string {
	trimmed := strings.TrimSpace(systemPath)
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

// RedfishExecutor executes power actions against Redfish.
type RedfishExecutor struct {
	baseConfig sharedredfish.Config
	creds      CredentialResolver
	systems    *SystemPathResolver
}

// NewRedfishExecutor creates an action executor backed by Redfish.
func NewRedfishExecutor(cfg sharedredfish.Config, creds CredentialResolver, systems *SystemPathResolver) *RedfishExecutor {
	if creds == nil {
		creds = EmptyCredentialResolver{}
	}
	if systems == nil {
		systems = NewSystemPathResolver()
	}

	return &RedfishExecutor{
		baseConfig: cfg,
		creds:      creds,
		systems:    systems,
	}
}

// ExecutePowerAction issues one Redfish reset action.
func (e *RedfishExecutor) ExecutePowerAction(ctx context.Context, req ExecutionRequest) error {
	client := e.client(req.InsecureSkipVerify)

	cred, err := e.creds.Resolve(ctx, req.CredentialID)
	if err != nil {
		return fmt.Errorf("resolving credential %q: %w", req.CredentialID, err)
	}

	systemPath, err := e.systems.Resolve(ctx, client, req.Endpoint, req.NodeID, cred)
	if err != nil {
		return classifyExecutionError(fmt.Errorf("resolving Redfish system path: %w", err))
	}

	if err := client.ResetSystem(ctx, req.Endpoint, systemPath, cred, req.Operation); err != nil {
		return classifyExecutionError(fmt.Errorf("issuing Redfish reset action: %w", err))
	}

	return nil
}

func (e *RedfishExecutor) client(insecureSkipVerify bool) RedfishAPI {
	cfg := e.baseConfig
	cfg.InsecureSkipVerify = insecureSkipVerify
	return sharedredfish.New(cfg)
}

// RedfishStateReader reads node power state from Redfish.
type RedfishStateReader struct {
	baseConfig sharedredfish.Config
	creds      CredentialResolver
	systems    *SystemPathResolver
}

// NewRedfishStateReader creates a verification reader backed by Redfish.
func NewRedfishStateReader(cfg sharedredfish.Config, creds CredentialResolver, systems *SystemPathResolver) *RedfishStateReader {
	if creds == nil {
		creds = EmptyCredentialResolver{}
	}
	if systems == nil {
		systems = NewSystemPathResolver()
	}

	return &RedfishStateReader{
		baseConfig: cfg,
		creds:      creds,
		systems:    systems,
	}
}

// ReadPowerState returns one node's current power state.
func (r *RedfishStateReader) ReadPowerState(ctx context.Context, req ExecutionRequest) (string, error) {
	client := r.client(req.InsecureSkipVerify)

	cred, err := r.creds.Resolve(ctx, req.CredentialID)
	if err != nil {
		return "", fmt.Errorf("resolving credential %q: %w", req.CredentialID, err)
	}

	systemPath, err := r.systems.Resolve(ctx, client, req.Endpoint, req.NodeID, cred)
	if err != nil {
		return "", fmt.Errorf("resolving Redfish system path: %w", err)
	}

	powerState, err := client.GetSystemPowerState(ctx, req.Endpoint, systemPath, cred)
	if err != nil {
		return "", fmt.Errorf("reading Redfish power state: %w", err)
	}

	return strings.TrimSpace(powerState), nil
}

func (r *RedfishStateReader) client(insecureSkipVerify bool) RedfishAPI {
	cfg := r.baseConfig
	cfg.InsecureSkipVerify = insecureSkipVerify
	return sharedredfish.New(cfg)
}

func classifyExecutionError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	lower := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"unexpected status 408",
		"unexpected status 429",
		"unexpected status 500",
		"unexpected status 502",
		"unexpected status 503",
		"unexpected status 504",
		"timeout",
		"temporary",
		"connection refused",
		"connection reset",
		"no such host",
	}
	for _, pattern := range retryablePatterns {
		if strings.Contains(lower, pattern) {
			return MarkRetryable(err)
		}
	}

	return err
}
