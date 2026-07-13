package oracle

import (
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// ErrObservationContract identifies invalid fixture or recorder evidence.
var ErrObservationContract = errors.New("oracle observation contract violation")

// ObservationContractError reports fixture evidence that cannot form an oracle observation.
type ObservationContractError struct {
	Err error
}

// Error implements error.
func (e *ObservationContractError) Error() string {
	return fmt.Sprintf("oracle observation contract violation: %v", e.Err)
}

// Unwrap exposes the underlying contract detail.
func (e *ObservationContractError) Unwrap() error {
	return e.Err
}

// Is makes every ObservationContractError identifiable as ErrObservationContract.
func (e *ObservationContractError) Is(target error) bool {
	return target == ErrObservationContract
}

// ObservationKind is the externally observable API server routing result.
type ObservationKind string

const (
	// ObservationCalled means the API server sent an AdmissionReview to the recorder.
	ObservationCalled ObservationKind = "called"
	// ObservationSkipped means the request succeeded without calling the recorder.
	ObservationSkipped ObservationKind = "skipped"
	// ObservationRejectedBeforeCall means the API server rejected the request without calling the recorder.
	ObservationRejectedBeforeCall ObservationKind = "rejected-before-call"
)

// Observation captures an oracle result without conflating setup and semantics.
type Observation struct {
	Kind       ObservationKind
	CallCount  int
	APIError   error
	LastReview RecordedReview
}

// Observe classifies the API result and recorder delta for one isolated request.
func Observe(before, after int, review RecordedReview, requestErr error) (Observation, error) {
	if before < 0 || after < before {
		return Observation{}, &ObservationContractError{
			Err: fmt.Errorf("invalid recorder counts before=%d after=%d", before, after),
		}
	}
	observation := Observation{
		CallCount:  after - before,
		APIError:   requestErr,
		LastReview: review,
	}
	switch {
	case observation.CallCount > 0:
		observation.Kind = ObservationCalled
	case isAPIStatus(requestErr):
		observation.Kind = ObservationRejectedBeforeCall
	case requestErr != nil:
		return Observation{}, &SetupError{
			Stage: SetupControlPlane,
			Err:   fmt.Errorf("submit oracle API request: %w", requestErr),
		}
	default:
		observation.Kind = ObservationSkipped
	}
	return observation, nil
}

// Compare checks a completed observation against the expected semantic result.
func Compare(expected ObservationKind, actual Observation) error {
	if !validObservationKind(expected) {
		return &ObservationContractError{Err: fmt.Errorf("unknown expected observation %q", expected)}
	}
	if !validObservationKind(actual.Kind) {
		return &ObservationContractError{Err: fmt.Errorf("unknown actual observation %q", actual.Kind)}
	}
	if actual.Kind == expected {
		return nil
	}
	return &SemanticMismatch{Expected: expected, Actual: actual.Kind}
}

func isAPIStatus(err error) bool {
	if err == nil {
		return false
	}
	var status apierrors.APIStatus
	return errors.As(err, &status)
}

func validObservationKind(kind ObservationKind) bool {
	switch kind {
	case ObservationCalled, ObservationSkipped, ObservationRejectedBeforeCall:
		return true
	default:
		return false
	}
}
