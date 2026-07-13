package oracle

import "fmt"

// SetupStage identifies the infrastructure phase that prevented an oracle run.
type SetupStage string

const (
	// SetupAssets identifies missing, invalid, or incorrectly versioned binaries.
	SetupAssets SetupStage = "assets"
	// SetupControlPlane identifies envtest control-plane startup failures.
	SetupControlPlane SetupStage = "control-plane"
	// SetupTLS identifies loopback Webhook TLS setup failures.
	SetupTLS SetupStage = "tls"
	// SetupResource identifies Kubernetes test-resource setup failures.
	SetupResource SetupStage = "resource"
)

// SetupError reports infrastructure failure separately from semantic mismatch.
type SetupError struct {
	Stage SetupStage
	Err   error
}

// Error implements error.
func (e *SetupError) Error() string {
	return fmt.Sprintf("oracle setup failed at %s: %v", e.Stage, e.Err)
}

// Unwrap exposes the underlying setup error.
func (e *SetupError) Unwrap() error {
	return e.Err
}

// SemanticMismatch reports a completed oracle observation that differs from the expected result.
type SemanticMismatch struct {
	Expected ObservationKind
	Actual   ObservationKind
}

// Error implements error.
func (e *SemanticMismatch) Error() string {
	return fmt.Sprintf("oracle semantic mismatch: got %s, want %s", e.Actual, e.Expected)
}
