package parity

import (
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
)

func TestValidateCasesRejectsOpenOracleAndUnstableTags(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*Case)
		wantFail bool
	}{
		{name: "valid"},
		{name: "open oracle", mutate: func(testCase *Case) { testCase.OracleType = "external" }, wantFail: true},
		{name: "golden trace is supplemental only", mutate: func(testCase *Case) { testCase.OracleType = "golden-trace" }, wantFail: true},
		{name: "unsorted tags", mutate: func(testCase *Case) { testCase.CoverageTags = []string{"z", "a"} }, wantFail: true},
		{name: "incomplete outcome", mutate: func(testCase *Case) {
			value := contract.OutcomeCalled
			testCase.Expected.Outcome = &value
		}, wantFail: true},
		{name: "invalid determination", mutate: func(testCase *Case) {
			testCase.Expected.Determination = "partial"
		}, wantFail: true},
		{name: "invalid outcome", mutate: func(testCase *Case) {
			testCase.Expected.Determination = contract.DeterminationDeterminate
			value := contract.Outcome("forwarded")
			testCase.Expected.Outcome = &value
		}, wantFail: true},
		{name: "invalid terminal reason", mutate: func(testCase *Case) {
			testCase.Expected.ReasonCode = "UNKNOWN_REASON"
		}, wantFail: true},
		{name: "invalid trace result", mutate: func(testCase *Case) {
			testCase.Expected.Trace[0].Result = "partial"
		}, wantFail: true},
		{name: "invalid diagnostic code", mutate: func(testCase *Case) {
			testCase.Expected.Diagnostics[0] = "UNKNOWN_DIAGNOSTIC"
		}, wantFail: true},
		{name: "incomplete trace has no terminal", mutate: func(testCase *Case) {
			testCase.Expected.Trace[0].Terminal = false
		}, wantFail: true},
		{name: "incomplete terminal reason differs", mutate: func(testCase *Case) {
			testCase.Expected.Trace[0].ReasonCode = contract.ReasonCodeNamespaceContextMissing
		}, wantFail: true},
		{name: "incomplete diagnostic omits terminal reason", mutate: func(testCase *Case) {
			testCase.Expected.Diagnostics[0] = contract.ReasonCodeNamespaceContextMissing
		}, wantFail: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testCase := validIncompleteCase()
			if test.mutate != nil {
				test.mutate(&testCase)
			}
			err := ValidateCases("test", []Case{testCase})
			if (err != nil) != test.wantFail {
				t.Errorf("ValidateCases() error = %v, wantFail %t", err, test.wantFail)
			}
		})
	}
}

func TestCompareEvaluationRejectsInvalidActual(t *testing.T) {
	testCase := validIncompleteCase()
	actual := validIncompleteEvaluation()
	actual.Determination = "partial"
	if err := CompareEvaluation(testCase, actual); err == nil || !strings.Contains(err.Error(), "validate actual evaluation") {
		t.Fatalf("CompareEvaluation() error = %v, want invalid actual rejection", err)
	}
}

func TestCompareEvaluationMismatchRedactsDiagnosticAndInputDetail(t *testing.T) {
	testCase := validIncompleteCase()
	testCase.Expected.Diagnostics = []contract.ReasonCode{contract.ReasonCodeNamespaceContextMissing}
	actual := validIncompleteEvaluation()
	actual.Trace[0].InputSummary = contract.InputSummary{"authorization": "sensitive-query"}
	actual.Diagnostics[0].Message = "sensitive-diagnostic"

	err := CompareEvaluation(testCase, actual)
	if err == nil {
		t.Fatal("CompareEvaluation() error = nil, want typed mismatch")
	}
	for _, sensitive := range []string{"sensitive-query", "sensitive-diagnostic"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Errorf("CompareEvaluation() error contains sensitive detail %q", sensitive)
		}
	}
	if !strings.Contains(err.Error(), "diagnostics got") {
		t.Errorf("CompareEvaluation() error = %v, want typed diagnostic mismatch", err)
	}
}

func validIncompleteCase() Case {
	return Case{
		Group:           "test",
		Scenario:        contract.Scenario{Metadata: contract.ScenarioMetadata{Name: "fixture"}},
		OracleType:      OracleIncompleteContract,
		OracleRationale: "fixture is intentionally incomplete",
		CoverageTags:    []string{"incomplete", "test"},
		Expected: ExpectedResult{
			Determination: contract.DeterminationIndeterminate,
			ReasonCode:    contract.ReasonCodeAuthorizationContextMissing,
			Trace: []TraceExpectation{{
				Stage:      "matchConditions",
				Result:     contract.TraceResultIndeterminate,
				ReasonCode: contract.ReasonCodeAuthorizationContextMissing,
				Terminal:   true,
			}},
			Diagnostics: []contract.ReasonCode{contract.ReasonCodeAuthorizationContextMissing},
		},
	}
}

func validIncompleteEvaluation() contract.WebhookEvaluation {
	return contract.WebhookEvaluation{
		Determination: contract.DeterminationIndeterminate,
		Trace: []contract.TraceStep{{
			Stage:        "matchConditions",
			InputSummary: contract.InputSummary{},
			Result:       contract.TraceResultIndeterminate,
			ReasonCode:   contract.ReasonCodeAuthorizationContextMissing,
			Terminal:     true,
		}},
		Diagnostics: []contract.Diagnostic{{
			Code:     contract.ReasonCodeAuthorizationContextMissing,
			Severity: contract.DiagnosticSeverityWarning,
		}},
	}
}
