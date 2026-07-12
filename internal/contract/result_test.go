package contract_test

import (
	"errors"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
)

func TestWebhookEvaluationValidateOutcome(t *testing.T) {
	t.Parallel()

	called := contract.OutcomeCalled
	tests := []struct {
		name          string
		determination contract.Determination
		outcome       *contract.Outcome
		wantError     bool
	}{
		{name: "determinate with outcome", determination: contract.DeterminationDeterminate, outcome: &called},
		{name: "determinate without optional outcome", determination: contract.DeterminationDeterminate},
		{name: "indeterminate without outcome", determination: contract.DeterminationIndeterminate},
		{name: "unsupported without outcome", determination: contract.DeterminationUnsupported},
		{name: "indeterminate with outcome", determination: contract.DeterminationIndeterminate, outcome: &called, wantError: true},
		{name: "unsupported with outcome", determination: contract.DeterminationUnsupported, outcome: &called, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			evaluation := contract.WebhookEvaluation{
				Determination: test.determination,
				Outcome:       test.outcome,
			}
			err := evaluation.ValidateOutcome()
			if got := errors.Is(err, contract.ErrOutcomeRequiresDeterminate); got != test.wantError {
				t.Errorf("ValidateOutcome() matched ErrOutcomeRequiresDeterminate = %t, want %t (err = %v)", got, test.wantError, err)
			}
		})
	}
}

func TestWebhookExpectationValidateOutcome(t *testing.T) {
	t.Parallel()

	skipped := contract.OutcomeSkipped
	expectation := contract.WebhookExpectation{
		Determination: contract.DeterminationUnsupported,
		Outcome:       &skipped,
	}

	if err := expectation.ValidateOutcome(); !errors.Is(err, contract.ErrOutcomeRequiresDeterminate) {
		t.Errorf("ValidateOutcome() error = %v, want ErrOutcomeRequiresDeterminate", err)
	}
}
