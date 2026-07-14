package contract_test

import (
	"errors"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
)

func TestResultVocabulary(t *testing.T) {
	t.Parallel()

	for _, phase := range []contract.EvaluationPhase{
		contract.EvaluationPhaseSnapshotRouting,
		contract.EvaluationPhaseMutatingInitialSnapshot,
	} {
		if !phase.IsValid() {
			t.Errorf("EvaluationPhase(%q).IsValid() = false, want true", phase)
		}
	}
	if contract.EvaluationPhase("future-phase").IsValid() {
		t.Error("EvaluationPhase(future-phase).IsValid() = true, want false")
	}

	for _, determination := range []contract.Determination{
		contract.DeterminationDeterminate,
		contract.DeterminationIndeterminate,
		contract.DeterminationUnsupported,
	} {
		if !determination.IsValid() {
			t.Errorf("Determination(%q).IsValid() = false, want true", determination)
		}
	}
	if contract.Determination("unknown").IsValid() {
		t.Error("Determination(unknown).IsValid() = true, want false")
	}

	for _, outcome := range []contract.Outcome{
		contract.OutcomeCalled,
		contract.OutcomeSkipped,
		contract.OutcomeRejectedBeforeCall,
	} {
		if !outcome.IsValid() {
			t.Errorf("Outcome(%q).IsValid() = false, want true", outcome)
		}
	}
	if contract.Outcome("unknown").IsValid() {
		t.Error("Outcome(unknown).IsValid() = true, want false")
	}

	for _, result := range []contract.TraceResult{
		contract.TraceResultMatch,
		contract.TraceResultNoMatch,
		contract.TraceResultTrue,
		contract.TraceResultFalse,
		contract.TraceResultError,
		contract.TraceResultIndeterminate,
		contract.TraceResultUnsupported,
		contract.TraceResultNotRun,
	} {
		if !result.IsValid() {
			t.Errorf("TraceResult(%q).IsValid() = false, want true", result)
		}
	}
	if contract.TraceResult("unknown").IsValid() {
		t.Error("TraceResult(unknown).IsValid() = true, want false")
	}

	for _, severity := range []contract.DiagnosticSeverity{
		contract.DiagnosticSeverityInfo,
		contract.DiagnosticSeverityWarning,
		contract.DiagnosticSeverityError,
	} {
		if !severity.IsValid() {
			t.Errorf("DiagnosticSeverity(%q).IsValid() = false, want true", severity)
		}
	}
	if contract.DiagnosticSeverity("fatal").IsValid() {
		t.Error("DiagnosticSeverity(fatal).IsValid() = true, want false")
	}
}

func TestEvaluationResultRequiresCanonicalEnvelope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*contract.EvaluationResult)
		valid     bool
		wantError error
	}{
		{name: "validating snapshot", valid: true},
		{name: "mutating initial snapshot", mutate: func(result *contract.EvaluationResult) {
			result.ConfigurationKind = contract.ConfigurationKindMutating
			result.EvaluationPhase = contract.EvaluationPhaseMutatingInitialSnapshot
		}, valid: true},
		{name: "unsupported schema", mutate: func(result *contract.EvaluationResult) { result.SchemaVersion = "future" }},
		{name: "unsupported profile", mutate: func(result *contract.EvaluationResult) { result.CompatibilityProfile.ID = "future" }},
		{name: "unsupported configuration kind", mutate: func(result *contract.EvaluationResult) { result.ConfigurationKind = "OtherConfiguration" }},
		{name: "webhook configuration kind mismatch", mutate: func(result *contract.EvaluationResult) {
			result.Webhooks = []contract.WebhookEvaluation{{ConfigurationKind: contract.ConfigurationKindMutating}}
		}, wantError: contract.ErrConfigurationKindMismatch},
		{name: "mutating generic snapshot", mutate: func(result *contract.EvaluationResult) { result.ConfigurationKind = contract.ConfigurationKindMutating }},
		{name: "validating mutating phase", mutate: func(result *contract.EvaluationResult) {
			result.EvaluationPhase = contract.EvaluationPhaseMutatingInitialSnapshot
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := contract.EvaluationResult{
				SchemaVersion:        contract.ResultSchemaVersion,
				CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
				ConfigurationKind:    contract.ConfigurationKindValidating,
				EvaluationPhase:      contract.EvaluationPhaseSnapshotRouting,
			}
			if test.mutate != nil {
				test.mutate(&result)
			}
			err := result.Validate()
			gotValid := err == nil
			if gotValid != test.valid {
				t.Errorf("Validate() valid = %t, want %t (error = %v)", gotValid, test.valid, err)
			}
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Errorf("Validate() error = %v, want %v", err, test.wantError)
			}
		})
	}
}

func TestWebhookEvaluationValidateOutcome(t *testing.T) {
	t.Parallel()

	called := contract.OutcomeCalled
	skipped := contract.OutcomeSkipped
	rejected := contract.OutcomeRejectedBeforeCall
	invalid := contract.Outcome("invalid")
	tests := []struct {
		name          string
		determination contract.Determination
		outcome       *contract.Outcome
		wantError     error
	}{
		{name: "determinate called", determination: contract.DeterminationDeterminate, outcome: &called},
		{name: "determinate skipped", determination: contract.DeterminationDeterminate, outcome: &skipped},
		{name: "determinate rejected before call", determination: contract.DeterminationDeterminate, outcome: &rejected},
		{name: "determinate without outcome", determination: contract.DeterminationDeterminate, wantError: contract.ErrDeterminateRequiresOutcome},
		{name: "determinate with invalid outcome", determination: contract.DeterminationDeterminate, outcome: &invalid, wantError: contract.ErrInvalidEnumValue},
		{name: "indeterminate without outcome", determination: contract.DeterminationIndeterminate},
		{name: "unsupported without outcome", determination: contract.DeterminationUnsupported},
		{name: "indeterminate with outcome", determination: contract.DeterminationIndeterminate, outcome: &called, wantError: contract.ErrOutcomeRequiresDeterminate},
		{name: "unsupported with outcome", determination: contract.DeterminationUnsupported, outcome: &called, wantError: contract.ErrOutcomeRequiresDeterminate},
		{name: "invalid determination", determination: contract.Determination("unknown"), wantError: contract.ErrInvalidEnumValue},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			evaluation := contract.WebhookEvaluation{
				Determination: test.determination,
				Outcome:       test.outcome,
			}
			err := evaluation.ValidateOutcome()
			assertErrorIs(t, err, test.wantError)
		})
	}
}

func TestWebhookExpectationValidateOutcome(t *testing.T) {
	t.Parallel()

	called := contract.OutcomeCalled
	invalid := contract.Outcome("invalid")
	tests := []struct {
		name          string
		determination contract.Determination
		outcome       *contract.Outcome
		wantError     error
	}{
		{name: "determinate outcome assertion omitted", determination: contract.DeterminationDeterminate},
		{name: "determinate outcome asserted", determination: contract.DeterminationDeterminate, outcome: &called},
		{name: "indeterminate outcome assertion omitted", determination: contract.DeterminationIndeterminate},
		{name: "unsupported outcome assertion omitted", determination: contract.DeterminationUnsupported},
		{name: "indeterminate with outcome", determination: contract.DeterminationIndeterminate, outcome: &called, wantError: contract.ErrOutcomeRequiresDeterminate},
		{name: "determinate invalid outcome", determination: contract.DeterminationDeterminate, outcome: &invalid, wantError: contract.ErrInvalidEnumValue},
		{name: "invalid determination", determination: contract.Determination("unknown"), wantError: contract.ErrInvalidEnumValue},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			expectation := contract.WebhookExpectation{
				Determination: test.determination,
				Outcome:       test.outcome,
			}
			assertErrorIs(t, expectation.ValidateOutcome(), test.wantError)
		})
	}
}

func TestWebhookExpectationValidateReasonCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		reason    contract.ReasonCode
		wantError error
	}{
		{name: "reason assertion omitted"},
		{name: "registered terminal reason", reason: contract.ReasonCodeMatchConditionsTrue},
		{name: "unregistered terminal reason", reason: "UNKNOWN", wantError: contract.ErrUnregisteredReasonCode},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			expectation := contract.WebhookExpectation{
				Determination:      contract.DeterminationDeterminate,
				TerminalReasonCode: test.reason,
			}
			assertErrorIs(t, expectation.Validate(), test.wantError)
		})
	}
}

func TestWebhookEvaluationRequiresOneTerminalCause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*contract.WebhookEvaluation)
		wantError error
	}{
		{name: "one terminal cause"},
		{
			name: "no terminal cause",
			mutate: func(evaluation *contract.WebhookEvaluation) {
				evaluation.Trace[0].Terminal = false
			},
			wantError: contract.ErrTerminalTraceRequired,
		},
		{
			name: "multiple terminal causes",
			mutate: func(evaluation *contract.WebhookEvaluation) {
				evaluation.Trace = append(evaluation.Trace, contract.TraceStep{
					Result:     contract.TraceResultMatch,
					ReasonCode: contract.ReasonCodeRuleMatch,
					Terminal:   true,
				})
			},
			wantError: contract.ErrMultipleTerminalTraceSteps,
		},
		{
			name: "terminal cause conflicts with determination",
			mutate: func(evaluation *contract.WebhookEvaluation) {
				evaluation.Trace[0].Result = contract.TraceResultIndeterminate
				evaluation.Trace[0].ReasonCode = contract.ReasonCodeNamespaceContextMissing
			},
			wantError: contract.ErrInvalidTraceState,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			evaluation := validDeterminateEvaluation()
			if test.mutate != nil {
				test.mutate(&evaluation)
			}
			assertErrorIs(t, evaluation.Validate(), test.wantError)
		})
	}
}

func TestIncompleteEvaluationTerminalVocabulary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		determination contract.Determination
		result        contract.TraceResult
		reason        contract.ReasonCode
	}{
		{
			name:          "indeterminate",
			determination: contract.DeterminationIndeterminate,
			result:        contract.TraceResultIndeterminate,
			reason:        contract.ReasonCodeNamespaceContextMissing,
		},
		{
			name:          "unsupported",
			determination: contract.DeterminationUnsupported,
			result:        contract.TraceResultUnsupported,
			reason:        contract.ReasonCodeCapabilityOutsideProfile,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			evaluation := contract.WebhookEvaluation{
				Determination: test.determination,
				Trace: []contract.TraceStep{
					{Result: test.result, ReasonCode: test.reason, Terminal: true},
				},
			}
			if err := evaluation.Validate(); err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestEvaluationResultValidateWrapsStructuredError(t *testing.T) {
	t.Parallel()

	result := contract.EvaluationResult{
		SchemaVersion:        contract.ResultSchemaVersion,
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		ConfigurationKind:    contract.ConfigurationKindValidating,
		EvaluationPhase:      contract.EvaluationPhaseSnapshotRouting,
		Webhooks: []contract.WebhookEvaluation{
			{
				ConfigurationKind: contract.ConfigurationKindValidating,
				Determination:     contract.DeterminationDeterminate,
				Outcome:           outcomePointer(contract.OutcomeCalled),
				Trace: []contract.TraceStep{
					{Result: contract.TraceResultTrue, ReasonCode: contract.ReasonCode("UNKNOWN"), Terminal: true},
				},
			},
		},
	}

	err := result.Validate()
	if !errors.Is(err, contract.ErrUnregisteredReasonCode) {
		t.Fatalf("Validate() error = %v, want ErrUnregisteredReasonCode", err)
	}
	var validationError *contract.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("errors.As(%v, *ValidationError) = false, want true", err)
	}
	if validationError.Field != "reasonCode" {
		t.Errorf("ValidationError.Field = %q, want %q", validationError.Field, "reasonCode")
	}
}

func validDeterminateEvaluation() contract.WebhookEvaluation {
	return contract.WebhookEvaluation{
		Determination: contract.DeterminationDeterminate,
		Outcome:       outcomePointer(contract.OutcomeCalled),
		Trace: []contract.TraceStep{
			{
				Result:     contract.TraceResultTrue,
				ReasonCode: contract.ReasonCodeMatchConditionTrue,
				Terminal:   true,
			},
		},
	}
}

func outcomePointer(outcome contract.Outcome) *contract.Outcome {
	return &outcome
}

func assertErrorIs(t *testing.T, got, want error) {
	t.Helper()

	if want == nil {
		if got != nil {
			t.Errorf("error = %v, want nil", got)
		}
		return
	}
	if !errors.Is(got, want) {
		t.Errorf("error = %v, want errors.Is(..., %v)", got, want)
	}
}
