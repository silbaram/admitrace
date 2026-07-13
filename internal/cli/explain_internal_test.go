package cli

import (
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
)

func TestExplanationExitCodePreservesInternalErrorPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result contract.EvaluationResult
	}{
		{
			name: "top-level diagnostic",
			result: contract.EvaluationResult{
				Diagnostics: []contract.Diagnostic{{Code: contract.ReasonCodeInternalError}},
				Webhooks:    []contract.WebhookEvaluation{{Determination: contract.DeterminationIndeterminate}},
			},
		},
		{
			name: "later webhook diagnostic",
			result: contract.EvaluationResult{
				Webhooks: []contract.WebhookEvaluation{
					{Determination: contract.DeterminationIndeterminate},
					{
						Determination: contract.DeterminationDeterminate,
						Diagnostics:   []contract.Diagnostic{{Code: contract.ReasonCodeInternalError}},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := explanationExitCode(test.result); got != ExitInternalError {
				t.Errorf("explanationExitCode() = %d, want %d", got, ExitInternalError)
			}
		})
	}
}
