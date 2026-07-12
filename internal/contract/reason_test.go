package contract_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
)

func TestReasonCodeRegistry(t *testing.T) {
	t.Parallel()

	want := []contract.ReasonDefinition{
		{Code: contract.ReasonCodeAdmissionConfigurationExcluded, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeCapabilityOutsideProfile, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeEvaluationProblemDiscarded, Disposition: contract.ReasonDispositionDiscarded},
		{Code: contract.ReasonCodeEvaluationProblemPending, Disposition: contract.ReasonDispositionPending},
		{Code: contract.ReasonCodeEquivalenceContextMissing, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeInternalError, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeInvalidInput, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeKubernetesEvaluationError, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeMatchConditionsTrue, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeMatchConditionTrue, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeNamespaceContextMissing, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeNamespaceSelectorMatch, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeNamespaceSelectorNoMatch, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeObjectSelectorMatch, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeObjectSelectorNoMatch, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeRuleMatch, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeRuleNoMatch, Disposition: contract.ReasonDispositionCompleted},
		{Code: contract.ReasonCodeStageNotRun, Disposition: contract.ReasonDispositionNotRun},
	}

	if err := contract.ValidateReasonCodeRegistry(); err != nil {
		t.Fatalf("ValidateReasonCodeRegistry() error = %v, want nil", err)
	}
	if got := contract.ReasonCodeRegistry(); !reflect.DeepEqual(got, want) {
		t.Errorf("ReasonCodeRegistry() = %#v, want %#v", got, want)
	}

	seen := make(map[contract.ReasonCode]struct{}, len(want))
	for _, definition := range contract.ReasonCodeRegistry() {
		if _, duplicate := seen[definition.Code]; duplicate {
			t.Errorf("ReasonCodeRegistry() contains duplicate code %q", definition.Code)
		}
		seen[definition.Code] = struct{}{}
		if !definition.Code.IsRegistered() {
			t.Errorf("ReasonCode(%q).IsRegistered() = false, want true", definition.Code)
		}
	}
	if contract.ReasonCode("UNKNOWN").IsRegistered() {
		t.Error("ReasonCode(UNKNOWN).IsRegistered() = true, want false")
	}
}

func TestReasonCodeRegistryReturnsCallerOwnedCopies(t *testing.T) {
	t.Parallel()

	definitions := contract.ReasonCodeRegistry()
	codes := contract.RegisteredReasonCodes()
	definitions[0] = contract.ReasonDefinition{Code: "MUTATED", Disposition: contract.ReasonDispositionPending}
	codes[0] = "MUTATED"

	gotDefinitions := contract.ReasonCodeRegistry()
	gotCodes := contract.RegisteredReasonCodes()
	if gotDefinitions[0].Code == "MUTATED" {
		t.Error("ReasonCodeRegistry() retained a caller mutation")
	}
	if gotCodes[0] == "MUTATED" {
		t.Error("RegisteredReasonCodes() retained a caller mutation")
	}
	if len(gotDefinitions) != len(gotCodes) {
		t.Errorf("registry lengths = (%d, %d), want equal", len(gotDefinitions), len(gotCodes))
	}
	for i := range gotDefinitions {
		if gotDefinitions[i].Code != gotCodes[i] {
			t.Errorf("registry code[%d] = %q, RegisteredReasonCodes()[%d] = %q", i, gotDefinitions[i].Code, i, gotCodes[i])
		}
	}
}

func TestTraceStepDeferredAndNotRunStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		step contract.TraceStep
	}{
		{
			name: "pending evaluation error",
			step: contract.TraceStep{
				Result:     contract.TraceResultError,
				ReasonCode: contract.ReasonCodeEvaluationProblemPending,
				Pending:    true,
			},
		},
		{
			name: "pending missing context",
			step: contract.TraceStep{
				Result:     contract.TraceResultIndeterminate,
				ReasonCode: contract.ReasonCodeEvaluationProblemPending,
				Pending:    true,
			},
		},
		{
			name: "discarded evaluation error",
			step: contract.TraceStep{
				Result:     contract.TraceResultError,
				ReasonCode: contract.ReasonCodeEvaluationProblemDiscarded,
				Discarded:  true,
			},
		},
		{
			name: "discarded missing context",
			step: contract.TraceStep{
				Result:     contract.TraceResultIndeterminate,
				ReasonCode: contract.ReasonCodeEvaluationProblemDiscarded,
				Discarded:  true,
			},
		},
		{
			name: "stage not run",
			step: contract.TraceStep{
				Result:     contract.TraceResultNotRun,
				ReasonCode: contract.ReasonCodeStageNotRun,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := test.step.Validate(); err != nil {
				t.Errorf("Validate() error = %v, want nil", err)
			}
		})
	}

	pending := tests[0].step
	discarded := tests[2].step
	notRun := tests[4].step
	if pending.ReasonCode == discarded.ReasonCode || pending.ReasonCode == notRun.ReasonCode || discarded.ReasonCode == notRun.ReasonCode {
		t.Error("pending, discarded, and not-run reason codes must be distinct")
	}
	if !pending.Pending || pending.Discarded || discarded.Pending || !discarded.Discarded || notRun.Pending || notRun.Discarded {
		t.Error("pending, discarded, and not-run trace flags are ambiguous")
	}
}

func TestTraceStepRejectsAmbiguousState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		step      contract.TraceStep
		wantError error
	}{
		{
			name:      "unregistered reason",
			step:      contract.TraceStep{Result: contract.TraceResultMatch, ReasonCode: "UNKNOWN"},
			wantError: contract.ErrUnregisteredReasonCode,
		},
		{
			name:      "pending and discarded",
			step:      contract.TraceStep{Result: contract.TraceResultError, ReasonCode: contract.ReasonCodeEvaluationProblemPending, Pending: true, Discarded: true},
			wantError: contract.ErrInvalidTraceState,
		},
		{
			name:      "pending reason without pending flag",
			step:      contract.TraceStep{Result: contract.TraceResultError, ReasonCode: contract.ReasonCodeEvaluationProblemPending},
			wantError: contract.ErrInvalidTraceState,
		},
		{
			name:      "pending reason with completed result",
			step:      contract.TraceStep{Result: contract.TraceResultTrue, ReasonCode: contract.ReasonCodeEvaluationProblemPending, Pending: true},
			wantError: contract.ErrInvalidTraceState,
		},
		{
			name:      "discarded reason without discarded flag",
			step:      contract.TraceStep{Result: contract.TraceResultError, ReasonCode: contract.ReasonCodeEvaluationProblemDiscarded},
			wantError: contract.ErrInvalidTraceState,
		},
		{
			name:      "not-run reason with executed result",
			step:      contract.TraceStep{Result: contract.TraceResultFalse, ReasonCode: contract.ReasonCodeStageNotRun},
			wantError: contract.ErrInvalidTraceState,
		},
		{
			name:      "not-run result with completed reason",
			step:      contract.TraceStep{Result: contract.TraceResultNotRun, ReasonCode: contract.ReasonCodeRuleMatch},
			wantError: contract.ErrInvalidTraceState,
		},
		{
			name:      "terminal pending problem",
			step:      contract.TraceStep{Result: contract.TraceResultError, ReasonCode: contract.ReasonCodeEvaluationProblemPending, Terminal: true, Pending: true},
			wantError: contract.ErrInvalidTraceState,
		},
		{
			name:      "terminal discarded problem",
			step:      contract.TraceStep{Result: contract.TraceResultIndeterminate, ReasonCode: contract.ReasonCodeEvaluationProblemDiscarded, Terminal: true, Discarded: true},
			wantError: contract.ErrInvalidTraceState,
		},
		{
			name:      "terminal stage not run",
			step:      contract.TraceStep{Result: contract.TraceResultNotRun, ReasonCode: contract.ReasonCodeStageNotRun, Terminal: true},
			wantError: contract.ErrInvalidTraceState,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := test.step.Validate(); !errors.Is(err, test.wantError) {
				t.Errorf("Validate() error = %v, want errors.Is(..., %v)", err, test.wantError)
			}
		})
	}
}

func TestDiagnosticCodeIsIndependentFromMessage(t *testing.T) {
	t.Parallel()

	diagnostics := []contract.Diagnostic{
		{Code: contract.ReasonCodeInvalidInput, Severity: contract.DiagnosticSeverityError, Message: "first wording"},
		{Code: contract.ReasonCodeInvalidInput, Severity: contract.DiagnosticSeverityError, Message: "revised wording"},
	}
	for i, diagnostic := range diagnostics {
		if err := diagnostic.Validate(); err != nil {
			t.Fatalf("diagnostics[%d].Validate() error = %v, want nil", i, err)
		}
	}
	if diagnostics[0].Code != diagnostics[1].Code {
		t.Errorf("diagnostic codes = (%q, %q), want equal", diagnostics[0].Code, diagnostics[1].Code)
	}
}
