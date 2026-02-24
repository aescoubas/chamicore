package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"git.cscs.ch/openchami/chamicore-lib/redfish"
)

const (
	defaultVerificationWindow = 90 * time.Second
	defaultVerificationPoll   = 2 * time.Second
)

var (
	// ErrUnsupportedOperation indicates the operation has no verification expectation mapping.
	ErrUnsupportedOperation = errors.New("unsupported operation")
	// ErrVerificationTimeout indicates verification did not reach expected final state in time.
	ErrVerificationTimeout = errors.New("verification timed out")
)

// PowerStateReader reads the current node power state.
type PowerStateReader interface {
	ReadPowerState(ctx context.Context, req ExecutionRequest) (string, error)
}

// VerifyConfig controls final-state verification behavior.
type VerifyConfig struct {
	Window       time.Duration
	PollInterval time.Duration
}

// Verifier validates that a power operation reached the expected final state.
type Verifier struct {
	reader       PowerStateReader
	window       time.Duration
	pollInterval time.Duration
}

// NewVerifier creates a verifier.
func NewVerifier(reader PowerStateReader, cfg VerifyConfig) *Verifier {
	window := cfg.Window
	if window <= 0 {
		window = defaultVerificationWindow
	}

	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultVerificationPoll
	}
	if pollInterval > window {
		pollInterval = window
	}

	return &Verifier{
		reader:       reader,
		window:       window,
		pollInterval: pollInterval,
	}
}

// Verify polls power state until it matches the expected final state or times out.
func (v *Verifier) Verify(ctx context.Context, req ExecutionRequest) (string, error) {
	expectedState, err := expectedFinalPowerState(req.Operation)
	if err != nil {
		return "", err
	}

	verifyCtx, cancel := context.WithTimeout(ctx, v.window)
	defer cancel()

	lastState := ""
	for {
		state, readErr := v.reader.ReadPowerState(verifyCtx, req)
		if readErr != nil {
			if verifyCtx.Err() != nil {
				return strings.TrimSpace(lastState), verifyCtx.Err()
			}
			return strings.TrimSpace(lastState), fmt.Errorf("reading power state: %w", readErr)
		}

		lastState = strings.TrimSpace(state)
		if strings.EqualFold(lastState, expectedState) {
			return lastState, nil
		}

		timer := time.NewTimer(v.pollInterval)
		select {
		case <-verifyCtx.Done():
			timer.Stop()
			return strings.TrimSpace(lastState), fmt.Errorf(
				"%w: expected %q, last %q",
				ErrVerificationTimeout,
				expectedState,
				strings.TrimSpace(lastState),
			)
		case <-timer.C:
		}
	}
}

func expectedFinalPowerState(operation redfish.ResetOperation) (string, error) {
	switch operation {
	case redfish.ResetOperationOn,
		redfish.ResetOperationGracefulRestart,
		redfish.ResetOperationForceRestart,
		redfish.ResetOperationNMI:
		return "On", nil
	case redfish.ResetOperationForceOff,
		redfish.ResetOperationGracefulShutdown:
		return "Off", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedOperation, operation)
	}
}
