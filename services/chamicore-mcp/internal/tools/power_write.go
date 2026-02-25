package tools

import (
	"context"
	"strings"
	"time"

	powerclient "git.cscs.ch/openchami/chamicore-power/pkg/client"
	powertypes "git.cscs.ch/openchami/chamicore-power/pkg/types"
)

const (
	defaultPowerWaitTimeout      = 90 * time.Second
	defaultPowerWaitPollInterval = 2 * time.Second
)

func (r *Runner) powerTransitionsCreate(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		Operation       string   `json:"operation"`
		Nodes           []string `json:"nodes,omitempty"`
		Groups          []string `json:"groups,omitempty"`
		DryRun          bool     `json:"dry_run,omitempty"`
		RequestID       string   `json:"request_id,omitempty"`
		Confirm         *bool    `json:"confirm,omitempty"`
		DryRunCompat    *bool    `json:"dryRun,omitempty"`
		RequestIDCompat string   `json:"requestID,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}

	operation := strings.TrimSpace(req.Operation)
	if operation == "" {
		return nil, validationErrorf("operation is required")
	}

	dryRun := req.DryRun
	if req.DryRunCompat != nil {
		dryRun = *req.DryRunCompat
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(req.RequestIDCompat)
	}

	response, err := r.power.CreateTransition(ctx, powertypes.CreateTransitionRequest{
		Operation: operation,
		RequestID: requestID,
		Nodes:     trimStringList(req.Nodes),
		Groups:    trimStringList(req.Groups),
		DryRun:    dryRun,
	})
	if err != nil {
		return nil, mapExecutionError(err, "creating power transition")
	}
	return toMap(response)
}

func (r *Runner) powerTransitionsAbort(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ID      string `json:"id"`
		Confirm *bool  `json:"confirm,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	transitionID := strings.TrimSpace(req.ID)
	if transitionID == "" {
		return nil, validationErrorf("id is required")
	}

	response, err := r.power.AbortTransition(ctx, transitionID)
	if err != nil {
		return nil, mapExecutionError(err, "aborting power transition")
	}
	return toMap(response)
}

func (r *Runner) powerTransitionsWait(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ID                        string `json:"id"`
		TimeoutSeconds            *int   `json:"timeout_seconds,omitempty"`
		PollIntervalSeconds       *int   `json:"poll_interval_seconds,omitempty"`
		TimeoutSecondsCompat      *int   `json:"timeoutSeconds,omitempty"`
		PollIntervalSecondsCompat *int   `json:"pollIntervalSeconds,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	transitionID := strings.TrimSpace(req.ID)
	if transitionID == "" {
		return nil, validationErrorf("id is required")
	}

	timeout := defaultPowerWaitTimeout
	timeoutSeconds := firstOptionalInt(req.TimeoutSeconds, req.TimeoutSecondsCompat)
	if timeoutSeconds != nil {
		if *timeoutSeconds <= 0 {
			return nil, validationErrorf("timeout_seconds must be > 0")
		}
		timeout = time.Duration(*timeoutSeconds) * time.Second
	}

	interval := defaultPowerWaitPollInterval
	pollSeconds := firstOptionalInt(req.PollIntervalSeconds, req.PollIntervalSecondsCompat)
	if pollSeconds != nil {
		if *pollSeconds <= 0 {
			return nil, validationErrorf("poll_interval_seconds must be > 0")
		}
		interval = time.Duration(*pollSeconds) * time.Second
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	response, err := r.power.WaitTransition(waitCtx, transitionID, powerclient.WaitTransitionOptions{
		Interval: interval,
	})
	if err != nil {
		return nil, mapExecutionError(err, "waiting for power transition")
	}
	return toMap(response)
}

func firstOptionalInt(primary, fallback *int) *int {
	if primary != nil {
		return primary
	}
	return fallback
}
