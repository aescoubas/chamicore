// Package client provides a typed HTTP client SDK for chamicore-power.
package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
	baseclient "git.cscs.ch/openchami/chamicore-lib/httputil/client"
	"git.cscs.ch/openchami/chamicore-power/pkg/types"
)

const (
	defaultTimeout          = 30 * time.Second
	defaultMaxRetries       = 3
	defaultWaitPollInterval = 2 * time.Second
	transitionPathPrefix    = "/power/v1/transitions"
	powerStatusPath         = "/power/v1/power-status"
	actionOnPath            = "/power/v1/actions/on"
	actionOffPath           = "/power/v1/actions/off"
	actionRebootPath        = "/power/v1/actions/reboot"
	actionResetPath         = "/power/v1/actions/reset"
	adminMappingSyncPath    = "/power/v1/admin/mappings/sync"
)

// Config holds power client configuration.
type Config struct {
	// TokenRefresh optionally resolves a token dynamically when Token is empty.
	TokenRefresh func(ctx context.Context) (string, error)
	// BaseURL is the root URL of the power API (for example: http://localhost:27775).
	BaseURL string
	// Token is the bearer token used for API requests.
	Token string
	// Timeout is the per-request timeout. Defaults to 30s.
	Timeout time.Duration
	// MaxRetries is the number of retry attempts for transient errors.
	MaxRetries int
}

// Client is the typed HTTP SDK for power APIs.
type Client struct {
	client  *baseclient.Client
	baseURL string
	cfg     Config
}

// ListTransitionsOptions configures transition list pagination.
type ListTransitionsOptions struct {
	Limit  int
	Offset int
}

// PowerStatusOptions configures GET /power/v1/power-status query parameters.
type PowerStatusOptions struct {
	Nodes  []string
	Groups []string
}

// WaitTransitionOptions configures polling behavior in WaitTransition.
type WaitTransitionOptions struct {
	Interval time.Duration
}

// New creates a new power client.
func New(cfg Config) (*Client, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("client: BaseURL is required")
	}
	baseURL = strings.TrimRight(baseURL, "/")

	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = defaultMaxRetries
	}
	cfg.BaseURL = baseURL

	return &Client{
		client: baseclient.New(baseclient.Config{
			BaseURL:      cfg.BaseURL,
			Token:        cfg.Token,
			TokenRefresh: cfg.TokenRefresh,
			Timeout:      cfg.Timeout,
			MaxRetries:   cfg.MaxRetries,
		}),
		baseURL: cfg.BaseURL,
		cfg:     cfg,
	}, nil
}

// ListTransitions returns transition resources ordered by newest first.
func (c *Client) ListTransitions(
	ctx context.Context,
	opts ListTransitionsOptions,
) (*httputil.ResourceList[types.Transition], error) {
	path := buildListTransitionsPath(opts)
	var result httputil.ResourceList[types.Transition]
	if err := c.client.Get(ctx, path, &result); err != nil {
		return nil, fmt.Errorf("listing transitions: %w", err)
	}
	return &result, nil
}

// CreateTransition starts a new asynchronous transition request.
func (c *Client) CreateTransition(
	ctx context.Context,
	req types.CreateTransitionRequest,
) (*httputil.Resource[types.Transition], error) {
	var result httputil.Resource[types.Transition]
	if err := c.client.Post(ctx, transitionPathPrefix, req, &result); err != nil {
		return nil, fmt.Errorf("creating transition: %w", err)
	}
	return &result, nil
}

// StartTransition is an alias for CreateTransition.
func (c *Client) StartTransition(
	ctx context.Context,
	req types.CreateTransitionRequest,
) (*httputil.Resource[types.Transition], error) {
	return c.CreateTransition(ctx, req)
}

// GetTransition returns one transition by ID.
func (c *Client) GetTransition(ctx context.Context, id string) (*httputil.Resource[types.Transition], error) {
	transitionID := strings.TrimSpace(id)
	if transitionID == "" {
		return nil, fmt.Errorf("transition id is required")
	}

	var result httputil.Resource[types.Transition]
	path := fmt.Sprintf("%s/%s", transitionPathPrefix, url.PathEscape(transitionID))
	if err := c.client.Get(ctx, path, &result); err != nil {
		return nil, fmt.Errorf("getting transition %q: %w", transitionID, err)
	}
	return &result, nil
}

// AbortTransition requests cancellation for an in-progress transition.
//
// The endpoint returns a transition body on 202 Accepted. The base client
// exposes DELETE without response decoding, so this method fetches the updated
// transition with a follow-up GET after successful abort submission.
func (c *Client) AbortTransition(ctx context.Context, id string) (*httputil.Resource[types.Transition], error) {
	transitionID := strings.TrimSpace(id)
	if transitionID == "" {
		return nil, fmt.Errorf("transition id is required")
	}

	path := fmt.Sprintf("%s/%s", transitionPathPrefix, url.PathEscape(transitionID))
	if err := c.client.Delete(ctx, path); err != nil {
		return nil, fmt.Errorf("aborting transition %q: %w", transitionID, err)
	}

	result, err := c.GetTransition(ctx, transitionID)
	if err != nil {
		return nil, fmt.Errorf("loading transition %q after abort: %w", transitionID, err)
	}
	return result, nil
}

// WaitTransition polls transition status until it reaches a terminal state.
func (c *Client) WaitTransition(
	ctx context.Context,
	id string,
	opts WaitTransitionOptions,
) (*httputil.Resource[types.Transition], error) {
	transitionID := strings.TrimSpace(id)
	if transitionID == "" {
		return nil, fmt.Errorf("transition id is required")
	}

	interval := opts.Interval
	if interval <= 0 {
		interval = defaultWaitPollInterval
	}

	for {
		transition, err := c.GetTransition(ctx, transitionID)
		if err != nil {
			return nil, fmt.Errorf("waiting transition %q: %w", transitionID, err)
		}

		if IsTransitionTerminalState(transition.Spec.State) {
			return transition, nil
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("waiting transition %q: %w", transitionID, ctx.Err())
		case <-timer.C:
		}
	}
}

// GetPowerStatus returns per-node power status for the requested targets.
func (c *Client) GetPowerStatus(
	ctx context.Context,
	opts PowerStatusOptions,
) (*httputil.Resource[types.PowerStatus], error) {
	var result httputil.Resource[types.PowerStatus]
	path := buildPowerStatusPath(opts)
	if err := c.client.Get(ctx, path, &result); err != nil {
		return nil, fmt.Errorf("getting power status: %w", err)
	}
	return &result, nil
}

// ActionOn starts an "On" operation for requested targets.
func (c *Client) ActionOn(ctx context.Context, req types.ActionRequest) (*httputil.Resource[types.Transition], error) {
	return c.startTransitionAction(ctx, actionOnPath, req, "on")
}

// ActionOff starts a "ForceOff" operation for requested targets.
func (c *Client) ActionOff(ctx context.Context, req types.ActionRequest) (*httputil.Resource[types.Transition], error) {
	return c.startTransitionAction(ctx, actionOffPath, req, "off")
}

// ActionReboot starts a "ForceRestart" operation for requested targets.
func (c *Client) ActionReboot(
	ctx context.Context,
	req types.ActionRequest,
) (*httputil.Resource[types.Transition], error) {
	return c.startTransitionAction(ctx, actionRebootPath, req, "reboot")
}

// ActionReset starts a caller-selected reset operation for requested targets.
func (c *Client) ActionReset(
	ctx context.Context,
	req types.ResetActionRequest,
) (*httputil.Resource[types.Transition], error) {
	return c.startTransitionAction(ctx, actionResetPath, req, "reset")
}

// TriggerMappingSync requests an immediate sync of local power topology mappings.
func (c *Client) TriggerMappingSync(ctx context.Context) (*httputil.Resource[types.MappingSyncTrigger], error) {
	var result httputil.Resource[types.MappingSyncTrigger]
	if err := c.client.Post(ctx, adminMappingSyncPath, nil, &result); err != nil {
		return nil, fmt.Errorf("triggering mapping sync: %w", err)
	}
	return &result, nil
}

// IsTransitionTerminalState reports whether transition state is final.
func IsTransitionTerminalState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case types.TransitionStateCompleted,
		types.TransitionStateFailed,
		types.TransitionStatePartial,
		types.TransitionStateCanceled:
		return true
	default:
		return false
	}
}

func (c *Client) startTransitionAction(
	ctx context.Context,
	path string,
	payload any,
	label string,
) (*httputil.Resource[types.Transition], error) {
	var result httputil.Resource[types.Transition]
	if err := c.client.Post(ctx, path, payload, &result); err != nil {
		return nil, fmt.Errorf("starting %s action: %w", label, err)
	}
	return &result, nil
}

func buildListTransitionsPath(opts ListTransitionsOptions) string {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		params.Set("offset", strconv.Itoa(opts.Offset))
	}

	if encoded := params.Encode(); encoded != "" {
		return transitionPathPrefix + "?" + encoded
	}
	return transitionPathPrefix
}

func buildPowerStatusPath(opts PowerStatusOptions) string {
	params := url.Values{}
	appendQueryValues(params, "nodes", opts.Nodes)
	appendQueryValues(params, "groups", opts.Groups)

	if encoded := params.Encode(); encoded != "" {
		return powerStatusPath + "?" + encoded
	}
	return powerStatusPath
}

func appendQueryValues(params url.Values, key string, values []string) {
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		params.Add(key, normalized)
	}
}
