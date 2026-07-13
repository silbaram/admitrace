package oracle

import (
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestObserve(t *testing.T) {
	requestError := apierrors.NewBadRequest("request rejected")
	tests := []struct {
		name       string
		before     int
		after      int
		requestErr error
		want       ObservationKind
	}{
		{name: "called", before: 1, after: 2, requestErr: requestError, want: ObservationCalled},
		{name: "skipped", before: 1, after: 1, want: ObservationSkipped},
		{name: "rejected before call", before: 1, after: 1, requestErr: requestError, want: ObservationRejectedBeforeCall},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Observe(test.before, test.after, RecordedReview{}, test.requestErr)
			if err != nil {
				t.Fatalf("Observe() error = %v", err)
			}
			if got.Kind != test.want {
				t.Errorf("Observation.Kind = %q, want %q", got.Kind, test.want)
			}
		})
	}
}

func TestObserveRejectsInvalidRecorderCounts(t *testing.T) {
	_, err := Observe(2, 1, RecordedReview{}, nil)
	var contractError *ObservationContractError
	if !errors.As(err, &contractError) {
		t.Fatalf("Observe() error = %T, want *ObservationContractError", err)
	}
	if !errors.Is(err, ErrObservationContract) {
		t.Fatalf("errors.Is(error, ErrObservationContract) = false, want true")
	}
	var setupError *SetupError
	if errors.As(err, &setupError) {
		t.Fatalf("Observe() error = %T, must not be *SetupError", err)
	}
	var mismatch *SemanticMismatch
	if errors.As(err, &mismatch) {
		t.Fatalf("Observe() error = %T, must not be *SemanticMismatch", err)
	}
}

func TestObserveClassifiesTransportFailureAsSetup(t *testing.T) {
	_, err := Observe(1, 1, RecordedReview{}, errors.New("connection reset"))
	var setupError *SetupError
	if !errors.As(err, &setupError) {
		t.Fatalf("Observe() error = %T, want *SetupError", err)
	}
	if setupError.Stage != SetupControlPlane {
		t.Errorf("SetupError.Stage = %q, want %q", setupError.Stage, SetupControlPlane)
	}
}

func TestCompareDistinguishesSemanticMismatch(t *testing.T) {
	err := Compare(ObservationCalled, Observation{Kind: ObservationSkipped})
	var mismatch *SemanticMismatch
	if !errors.As(err, &mismatch) {
		t.Fatalf("Compare() error = %T, want *SemanticMismatch", err)
	}
	var setupError *SetupError
	if errors.As(err, &setupError) {
		t.Fatalf("Compare() error = %T, must not be *SetupError", err)
	}
}

func TestCompareRejectsUnknownObservation(t *testing.T) {
	err := Compare(ObservationCalled, Observation{Kind: "unknown"})
	var contractError *ObservationContractError
	if !errors.As(err, &contractError) {
		t.Fatalf("Compare() error = %T, want *ObservationContractError", err)
	}
}
