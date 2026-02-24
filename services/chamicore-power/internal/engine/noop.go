package engine

import "context"

// NoopExecutor is a placeholder action executor used until the concrete
// Redfish/auth-backed execution path is wired.
type NoopExecutor struct{}

// ExecutePowerAction marks execution as successful without issuing any action.
func (NoopExecutor) ExecutePowerAction(ctx context.Context, req ExecutionRequest) error {
	_ = ctx
	_ = req
	return nil
}

// ExpectedStateReader is a placeholder verifier backend returning expected
// operation end-state deterministically.
type ExpectedStateReader struct{}

// ReadPowerState returns the expected end-state for the requested operation.
func (ExpectedStateReader) ReadPowerState(ctx context.Context, req ExecutionRequest) (string, error) {
	_ = ctx
	return expectedFinalPowerState(req.Operation)
}
