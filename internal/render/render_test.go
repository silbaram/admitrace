package render_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/render"
)

func TestGoldenResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result contract.EvaluationResult
	}{
		{name: "determinate", result: determinateResult()},
		{name: "indeterminate", result: indeterminateResult()},
		{name: "unsupported", result: unsupportedResult()},
	}
	renderers := []struct {
		name      string
		extension string
		render    func(contract.EvaluationResult) ([]byte, error)
	}{
		{name: "json", extension: "json", render: render.JSON},
		{name: "text", extension: "txt", render: render.Text},
	}

	for _, test := range tests {
		test := test
		for _, renderer := range renderers {
			renderer := renderer
			t.Run(test.name+"/"+renderer.name, func(t *testing.T) {
				t.Parallel()

				got, err := renderer.render(test.result)
				if err != nil {
					t.Fatalf("render() error = %v, want nil", err)
				}
				assertGolden(t, test.name+".golden."+renderer.extension, got)
			})
		}
	}
}

func TestRenderersRejectInvalidDiagnostic(t *testing.T) {
	t.Parallel()

	result := baseResult("invalid-diagnostic")
	result.Diagnostics = []contract.Diagnostic{
		{
			Code:       "UNREGISTERED",
			Severity:   contract.DiagnosticSeverityError,
			Message:    "invalid diagnostic",
			SourcePath: ".diagnostics[0]",
		},
	}
	tests := []struct {
		name   string
		render func(contract.EvaluationResult) ([]byte, error)
	}{
		{name: "json", render: render.JSON},
		{name: "text", render: render.Text},
	}

	var messages []string
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.render(result)
			if err == nil {
				t.Fatal("render() error = nil, want invalid input")
			}
			if got != nil {
				t.Errorf("render() output = %q, want nil", got)
			}
			if !errors.Is(err, contract.ErrInvalidInput) {
				t.Errorf("render() error = %v, want ErrInvalidInput", err)
			}
			var validationError *contract.ValidationError
			if !errors.As(err, &validationError) {
				t.Errorf("errors.As(%v, *ValidationError) = false, want true", err)
			}
			messages = append(messages, test.name+": "+err.Error()+"\n")
		})
	}
	if len(messages) != len(tests) {
		t.Fatalf("collected %d errors, want %d", len(messages), len(tests))
	}
	assertGolden(t, "invalid-diagnostic.golden.txt", []byte(strings.Join(messages, "")))
}

func TestJSONIsByteIdenticalAndDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	result := determinateResult()
	beforeDiagnostics := append([]contract.Diagnostic(nil), result.Diagnostics...)
	beforeWebhookDiagnostics := append([]contract.Diagnostic(nil), result.Webhooks[0].Diagnostics...)
	first, err := render.JSON(result)
	if err != nil {
		t.Fatalf("JSON() error = %v, want nil", err)
	}
	for i := 0; i < 20; i++ {
		got, err := render.JSON(result)
		if err != nil {
			t.Fatalf("JSON() iteration %d error = %v, want nil", i, err)
		}
		if !bytes.Equal(got, first) {
			t.Fatalf("JSON() iteration %d differs from first serialization", i)
		}
	}
	if !reflect.DeepEqual(result.Diagnostics, beforeDiagnostics) {
		t.Errorf("JSON() mutated top-level diagnostics: got %+v, want %+v", result.Diagnostics, beforeDiagnostics)
	}
	if !reflect.DeepEqual(result.Webhooks[0].Diagnostics, beforeWebhookDiagnostics) {
		t.Errorf("JSON() mutated webhook diagnostics: got %+v, want %+v", result.Webhooks[0].Diagnostics, beforeWebhookDiagnostics)
	}

	reordered := determinateResult()
	reverseDiagnostics(reordered.Diagnostics)
	reverseDiagnostics(reordered.Webhooks[0].Diagnostics)
	reordered.Webhooks[0].Trace[0].InputSummary = make(contract.InputSummary, 2)
	reordered.Webhooks[0].Trace[0].InputSummary["resource"] = "deployments"
	reordered.Webhooks[0].Trace[0].InputSummary["operation"] = "CREATE"
	got, err := render.JSON(reordered)
	if err != nil {
		t.Fatalf("JSON(reordered) error = %v, want nil", err)
	}
	if !bytes.Equal(got, first) {
		t.Error("JSON() differs when only diagnostic and map insertion order changes")
	}
	firstText, err := render.Text(result)
	if err != nil {
		t.Fatalf("Text() error = %v, want nil", err)
	}
	reorderedText, err := render.Text(reordered)
	if err != nil {
		t.Fatalf("Text(reordered) error = %v, want nil", err)
	}
	if !bytes.Equal(reorderedText, firstText) {
		t.Error("Text() differs when only diagnostic and map insertion order changes")
	}
}

func TestJSONPreservesWebhookTraceAndCollectionSemantics(t *testing.T) {
	t.Parallel()

	result := determinateResult()
	data, err := render.JSON(result)
	if err != nil {
		t.Fatalf("JSON() error = %v, want nil", err)
	}
	var decoded contract.EvaluationResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal JSON output: %v", err)
	}
	if got, want := []string{decoded.Webhooks[0].WebhookName, decoded.Webhooks[1].WebhookName}, []string{"zeta.example.com", "alpha.example.com"}; !reflect.DeepEqual(got, want) {
		t.Errorf("webhook order = %v, want %v", got, want)
	}
	if got, want := []string{decoded.Webhooks[0].Trace[0].Stage, decoded.Webhooks[0].Trace[1].Stage}, []string{"rules", "matchConditions"}; !reflect.DeepEqual(got, want) {
		t.Errorf("trace order = %v, want %v", got, want)
	}

	empty := baseResult("collection-semantics")
	empty.Webhooks = []contract.WebhookEvaluation{}
	empty.Diagnostics = nil
	data, err = render.JSON(empty)
	if err != nil {
		t.Fatalf("JSON(empty) error = %v, want nil", err)
	}
	text := string(data)
	if !strings.Contains(text, `"webhooks": []`) {
		t.Errorf("JSON(empty) = %s, want empty webhooks array", data)
	}
	if !strings.Contains(text, `"diagnostics": null`) {
		t.Errorf("JSON(empty) = %s, want null diagnostics", data)
	}
}

func TestTextMatchesCanonicalOutcomeAndTerminalReasons(t *testing.T) {
	t.Parallel()

	for _, result := range []contract.EvaluationResult{
		determinateResult(),
		indeterminateResult(),
		unsupportedResult(),
	} {
		text, err := render.Text(result)
		if err != nil {
			t.Fatalf("Text(%q) error = %v, want nil", result.ScenarioID, err)
		}
		view := string(text)
		for _, webhook := range result.Webhooks {
			wantOutcome := "outcome: null"
			if webhook.Outcome != nil {
				wantOutcome = "outcome: " + string(*webhook.Outcome)
			}
			if !strings.Contains(view, wantOutcome) {
				t.Errorf("Text(%q) missing %q", result.ScenarioID, wantOutcome)
			}
			for _, step := range webhook.Trace {
				if step.Terminal && !strings.Contains(view, "- "+string(step.ReasonCode)) {
					t.Errorf("Text(%q) missing terminal reason %q", result.ScenarioID, step.ReasonCode)
				}
			}
		}
	}
}

func determinateResult() contract.EvaluationResult {
	result := baseResult("determinate-case")
	called := contract.OutcomeCalled
	skipped := contract.OutcomeSkipped
	result.Webhooks = []contract.WebhookEvaluation{
		{
			ConfigurationKind: contract.ConfigurationKindValidating,
			WebhookName:       "zeta.example.com",
			WebhookIndex:      0,
			SourcePath:        ".configuration.validatingWebhookConfiguration.webhooks[0]",
			Determination:     contract.DeterminationDeterminate,
			Outcome:           &called,
			Trace: []contract.TraceStep{
				{
					Stage:        "rules",
					Sequence:     0,
					SourcePath:   ".configuration.validatingWebhookConfiguration.webhooks[0].rules",
					InputSummary: contract.InputSummary{"resource": "deployments", "operation": "CREATE"},
					Result:       contract.TraceResultMatch,
					ReasonCode:   contract.ReasonCodeRuleMatch,
				},
				{
					Stage:        "matchConditions",
					Sequence:     1,
					SourcePath:   ".configuration.validatingWebhookConfiguration.webhooks[0].matchConditions",
					InputSummary: contract.InputSummary{},
					Result:       contract.TraceResultTrue,
					ReasonCode:   contract.ReasonCodeMatchConditionsTrue,
					Terminal:     true,
				},
			},
			Diagnostics: []contract.Diagnostic{
				capabilityDiagnostic(".z", "transport", "no network request was made"),
				capabilityDiagnostic(".a", "negotiation", "not evaluated"),
			},
		},
		{
			ConfigurationKind: contract.ConfigurationKindValidating,
			WebhookName:       "alpha.example.com",
			WebhookIndex:      1,
			SourcePath:        ".configuration.validatingWebhookConfiguration.webhooks[1]",
			Determination:     contract.DeterminationDeterminate,
			Outcome:           &skipped,
			Trace: []contract.TraceStep{
				{
					Stage:        "rules",
					Sequence:     0,
					SourcePath:   ".configuration.validatingWebhookConfiguration.webhooks[1].rules",
					InputSummary: nil,
					Result:       contract.TraceResultNoMatch,
					ReasonCode:   contract.ReasonCodeRuleNoMatch,
					Terminal:     true,
				},
			},
			Diagnostics: []contract.Diagnostic{},
		},
	}
	result.Diagnostics = []contract.Diagnostic{
		capabilityDiagnostic(".scope.z", "reinvocation", "not evaluated"),
		capabilityDiagnostic(".scope.a", "patch chain", "not evaluated"),
	}
	return result
}

func indeterminateResult() contract.EvaluationResult {
	result := baseResult("indeterminate-case")
	result.Webhooks = []contract.WebhookEvaluation{
		{
			ConfigurationKind: contract.ConfigurationKindValidating,
			WebhookName:       "missing-namespace.example.com",
			WebhookIndex:      0,
			SourcePath:        ".configuration.validatingWebhookConfiguration.webhooks[0]",
			Determination:     contract.DeterminationIndeterminate,
			Trace: []contract.TraceStep{
				{
					Stage:        "namespaceSelector",
					Sequence:     0,
					SourcePath:   ".configuration.validatingWebhookConfiguration.webhooks[0].namespaceSelector",
					InputSummary: contract.InputSummary{"namespace": "payments"},
					Result:       contract.TraceResultIndeterminate,
					ReasonCode:   contract.ReasonCodeNamespaceContextMissing,
					Terminal:     true,
				},
			},
			Diagnostics: []contract.Diagnostic{
				{
					Code:       contract.ReasonCodeNamespaceContextMissing,
					Severity:   contract.DiagnosticSeverityWarning,
					Message:    "required fixture context is missing",
					SourcePath: ".configuration.validatingWebhookConfiguration.webhooks[0].namespaceSelector",
					MissingContext: &contract.MissingContextDetail{
						Context:   "namespace",
						Reference: "payments",
					},
				},
			},
		},
	}
	result.Diagnostics = []contract.Diagnostic{}
	return result
}

func unsupportedResult() contract.EvaluationResult {
	result := baseResult("unsupported-case")
	result.Webhooks = []contract.WebhookEvaluation{
		{
			ConfigurationKind: contract.ConfigurationKindValidating,
			WebhookName:       "selector.example.com",
			WebhookIndex:      0,
			SourcePath:        ".configuration.validatingWebhookConfiguration.webhooks[0]",
			Determination:     contract.DeterminationUnsupported,
			Trace: []contract.TraceStep{
				{
					Stage:        "matchConditions",
					Sequence:     0,
					SourcePath:   ".configuration.validatingWebhookConfiguration.webhooks[0].matchConditions[0]",
					InputSummary: contract.InputSummary{"expression": "redacted"},
					Result:       contract.TraceResultUnsupported,
					ReasonCode:   contract.ReasonCodeCapabilityOutsideProfile,
					Terminal:     true,
				},
			},
			Diagnostics: []contract.Diagnostic{
				capabilityDiagnostic(
					".configuration.validatingWebhookConfiguration.webhooks[0].matchConditions[0]",
					"CEL authorizer fieldSelector",
					"selector authorization is outside the offline profile",
				),
			},
		},
	}
	return result
}

func baseResult(scenarioID string) contract.EvaluationResult {
	return contract.EvaluationResult{
		SchemaVersion:        contract.ResultSchemaVersion,
		ScenarioID:           scenarioID,
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		EvaluationPhase:      contract.EvaluationPhaseSnapshotRouting,
		ConfigurationKind:    contract.ConfigurationKindValidating,
	}
}

func capabilityDiagnostic(sourcePath, capability, detail string) contract.Diagnostic {
	return contract.Diagnostic{
		Code:       contract.ReasonCodeCapabilityOutsideProfile,
		Severity:   contract.DiagnosticSeverityInfo,
		Message:    "evaluation scope is limited",
		SourcePath: sourcePath,
		UnsupportedCapability: &contract.UnsupportedCapabilityDetail{
			Capability: capability,
			Detail:     detail,
		},
	}
}

func reverseDiagnostics(diagnostics []contract.Diagnostic) {
	for left, right := 0, len(diagnostics)-1; left < right; left, right = left+1, right-1 {
		diagnostics[left], diagnostics[right] = diagnostics[right], diagnostics[left]
	}
}

func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()

	want, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %q: %v\ngot:\n%s", name, err, got)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden %q mismatch\ngot:\n%s\nwant:\n%s", name, got, want)
	}
}
