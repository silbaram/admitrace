package cli

import (
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
)

func TestSummarizeTestReportPreservesInternalPriority(t *testing.T) {
	t.Parallel()

	fixtures := []fixtureReport{
		{code: ExitIncompleteEvaluation},
		{code: ExitExpectationMismatch},
		{code: ExitInvalidInput},
		{code: ExitInternalError},
	}
	summary := summarizeTestReport(fixtures)
	if summary.ExitCode != ExitInternalError {
		t.Errorf("ExitCode = %d, want %d", summary.ExitCode, ExitInternalError)
	}
	if summary.Mismatched != 1 || summary.Invalid != 1 || summary.Incomplete != 1 || summary.Internal != 1 {
		t.Errorf("summary counts = %#v, want one of each failure", summary)
	}
}

func TestCompareExpectationsTreatsOnlyExactIncompleteAsExpected(t *testing.T) {
	t.Parallel()

	webhook := contract.WebhookEvaluation{
		WebhookName:   "policy.example.com",
		Determination: contract.DeterminationIndeterminate,
		Trace: []contract.TraceStep{{
			ReasonCode: contract.ReasonCodeNamespaceContextMissing,
			Terminal:   true,
		}},
	}
	tests := []struct {
		name         string
		expectations []contract.WebhookExpectation
		wantCode     ExitCode
	}{
		{name: "unasserted", wantCode: ExitIncompleteEvaluation},
		{
			name: "expected",
			expectations: []contract.WebhookExpectation{{
				WebhookName:   webhook.WebhookName,
				Determination: contract.DeterminationIndeterminate,
			}},
			wantCode: ExitSuccess,
		},
		{
			name: "different incomplete determination",
			expectations: []contract.WebhookExpectation{{
				WebhookName:   webhook.WebhookName,
				Determination: contract.DeterminationUnsupported,
			}},
			wantCode: ExitExpectationMismatch,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, codes := compareExpectations(test.expectations, []contract.WebhookEvaluation{webhook})
			if got := SelectExitCode(codes...); got != test.wantCode {
				t.Errorf("comparison exit code = %d, want %d", got, test.wantCode)
			}
		})
	}
}
