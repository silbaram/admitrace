package contract_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type goldenVersions struct {
	ScenarioAPIVersion   string                        `json:"scenarioAPIVersion"`
	ScenarioKind         string                        `json:"scenarioKind"`
	ResultSchemaVersion  string                        `json:"resultSchemaVersion"`
	CompatibilityProfile contract.CompatibilityProfile `json:"compatibilityProfile"`
}

type goldenVocabularies struct {
	ConfigurationKinds    []contract.ConfigurationKind    `json:"configurationKinds"`
	EvaluationPhases      []contract.EvaluationPhase      `json:"evaluationPhases"`
	Determinations        []contract.Determination        `json:"determinations"`
	Outcomes              []contract.Outcome              `json:"outcomes"`
	TraceResults          []contract.TraceResult          `json:"traceResults"`
	DiagnosticSeverities  []contract.DiagnosticSeverity   `json:"diagnosticSeverities"`
	AuthorizationVerdicts []contract.AuthorizationVerdict `json:"authorizationVerdicts"`
	FeatureGatePolicies   []contract.FeatureGatePolicy    `json:"featureGatePolicies"`
}

type goldenContract struct {
	Versions              goldenVersions                  `json:"versions"`
	Vocabularies          goldenVocabularies              `json:"vocabularies"`
	ConfigurationVariants []contract.WebhookConfiguration `json:"configurationVariants"`
	Scenario              contract.Scenario               `json:"scenario"`
	Result                contract.EvaluationResult       `json:"result"`
}

func TestContractGolden(t *testing.T) {
	t.Parallel()

	got, err := json.MarshalIndent(newGoldenContract(), "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", "contract.golden.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("contract drift detected; review and update %s intentionally\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func newGoldenContract() goldenContract {
	dryRun := false
	called := contract.OutcomeCalled

	validating := &kube136.ValidatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admissionregistration.k8s.io/v1",
			Kind:       string(contract.ConfigurationKindValidating),
		},
		ObjectMeta: metav1.ObjectMeta{Name: "routing.example.io"},
	}
	mutating := &kube136.MutatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admissionregistration.k8s.io/v1",
			Kind:       string(contract.ConfigurationKindMutating),
		},
		ObjectMeta: metav1.ObjectMeta{Name: "mutation.example.io"},
	}

	scenario := contract.Scenario{
		APIVersion:           contract.ScenarioAPIVersion,
		Kind:                 contract.ScenarioKind,
		Metadata:             contract.ScenarioMetadata{Name: "update-widget"},
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		Configuration:        contract.WebhookConfiguration{Validating: validating},
		Request: contract.AdmissionRequest{
			UID:       types.UID("request-001"),
			Kind:      metav1.GroupVersionKind{Group: "example.io", Version: "v1", Kind: "Widget"},
			Resource:  metav1.GroupVersionResource{Group: "example.io", Version: "v1", Resource: "widgets"},
			Name:      "demo",
			Namespace: "test",
			Operation: admissionv1.Update,
			Scope:     contract.RequestScopeNamespaced,
			UserInfo: authenticationv1.UserInfo{
				Username: "alice",
				Groups:   []string{"developers", "system:authenticated"},
				Extra: map[string]authenticationv1.ExtraValue{
					"scopes": {"edit", "read"},
				},
			},
			Object:    json.RawMessage(`{"apiVersion":"example.io/v1","kind":"Widget","metadata":{"name":"demo","namespace":"test"},"spec":{"replicas":2,"extension":{"enabled":true}}}`),
			OldObject: json.RawMessage("null"),
			DryRun:    &dryRun,
			Options:   json.RawMessage(`{"apiVersion":"meta.k8s.io/v1","kind":"UpdateOptions","fieldManager":"admitrace"}`),
		},
		ExternalContext: &contract.ExternalContext{
			Namespace: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test",
					Labels: map[string]string{"environment": "test", "team": "platform"},
				},
			},
			Authorization: []contract.AuthorizationDecision{
				{
					Query: contract.AuthorizationQuery{
						Resource: &contract.ResourceAuthorizationQuery{
							Verb:       "update",
							APIGroup:   "example.io",
							APIVersion: "v1",
							Resource:   "widgets",
							Namespace:  "test",
							Name:       "demo",
						},
					},
					Verdict: contract.AuthorizationVerdictAllow,
					Reason:  "fixture grants widget updates",
				},
				{
					Query: contract.AuthorizationQuery{
						NonResource: &contract.NonResourceAuthorizationQuery{Verb: "get", Path: "/healthz"},
					},
					Verdict: contract.AuthorizationVerdictNoOpinion,
				},
			},
			Equivalence: []contract.EquivalenceMapping{
				{
					Request: contract.ResourceReference{
						GVR: metav1.GroupVersionResource{Group: "example.io", Version: "v1", Resource: "widgets"},
					},
					Equivalents: []contract.EquivalentResource{
						{
							GVR: metav1.GroupVersionResource{Group: "example.io", Version: "v1beta1", Resource: "widgets"},
							GVK: metav1.GroupVersionKind{Group: "example.io", Version: "v1beta1", Kind: "Widget"},
						},
						{
							GVR: metav1.GroupVersionResource{Group: "example.io", Version: "v1alpha1", Resource: "widgets"},
							GVK: metav1.GroupVersionKind{Group: "example.io", Version: "v1alpha1", Kind: "Widget"},
						},
					},
				},
			},
		},
		Expectations: []contract.WebhookExpectation{
			{
				WebhookName:        "accept.example.io",
				Determination:      contract.DeterminationDeterminate,
				Outcome:            &called,
				TerminalReasonCode: "MATCH_CONDITIONS_TRUE",
			},
			{
				WebhookName:   "context.example.io",
				Determination: contract.DeterminationIndeterminate,
			},
		},
	}

	result := contract.EvaluationResult{
		SchemaVersion:        contract.ResultSchemaVersion,
		ScenarioID:           scenario.Metadata.Name,
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		EvaluationPhase:      contract.EvaluationPhaseSnapshotRouting,
		ConfigurationKind:    contract.ConfigurationKindValidating,
		Webhooks: []contract.WebhookEvaluation{
			{
				ConfigurationKind: contract.ConfigurationKindValidating,
				WebhookName:       "accept.example.io",
				WebhookIndex:      0,
				SourcePath:        ".configuration.validatingWebhookConfiguration.webhooks[0]",
				Determination:     contract.DeterminationDeterminate,
				Outcome:           &called,
				Trace: []contract.TraceStep{
					{
						Stage:        "rules",
						Sequence:     0,
						SourcePath:   ".configuration.validatingWebhookConfiguration.webhooks[0].rules[0]",
						InputSummary: contract.InputSummary{"operation": "UPDATE", "resource": "example.io/v1/widgets"},
						Result:       contract.TraceResultMatch,
						ReasonCode:   "RULE_MATCH",
					},
					{
						Stage:        "matchConditions",
						Sequence:     1,
						SourcePath:   ".configuration.validatingWebhookConfiguration.webhooks[0].matchConditions[0]",
						InputSummary: contract.InputSummary{"expression": "redacted", "name": "authorized"},
						Result:       contract.TraceResultTrue,
						ReasonCode:   "MATCH_CONDITION_TRUE",
						Terminal:     true,
					},
				},
				Diagnostics: []contract.Diagnostic{},
			},
			{
				ConfigurationKind: contract.ConfigurationKindValidating,
				WebhookName:       "context.example.io",
				WebhookIndex:      1,
				SourcePath:        ".configuration.validatingWebhookConfiguration.webhooks[1]",
				Determination:     contract.DeterminationIndeterminate,
				Trace: []contract.TraceStep{
					{
						Stage:        "namespaceSelector",
						Sequence:     0,
						SourcePath:   ".configuration.validatingWebhookConfiguration.webhooks[1].namespaceSelector",
						InputSummary: contract.InputSummary{"namespace": "test"},
						Result:       contract.TraceResultIndeterminate,
						ReasonCode:   "NAMESPACE_CONTEXT_MISSING",
						Terminal:     true,
					},
				},
				Diagnostics: []contract.Diagnostic{
					{
						Code:       "NAMESPACE_CONTEXT_MISSING",
						Severity:   contract.DiagnosticSeverityError,
						Message:    "namespace fixture is required",
						SourcePath: ".externalContext.namespace",
						MissingContext: &contract.MissingContextDetail{
							Context:   "namespace",
							Reference: "test",
						},
					},
				},
			},
		},
		Diagnostics: []contract.Diagnostic{
			{
				Code:       "CAPABILITY_OUTSIDE_PROFILE",
				Severity:   contract.DiagnosticSeverityWarning,
				Message:    "capability is outside the selected profile",
				SourcePath: ".configuration.validatingWebhookConfiguration.webhooks[1]",
				UnsupportedCapability: &contract.UnsupportedCapabilityDetail{
					Capability: "custom-feature-gate",
					Detail:     "only Kubernetes default feature gates are supported",
				},
			},
		},
	}

	return goldenContract{
		Versions: goldenVersions{
			ScenarioAPIVersion:   contract.ScenarioAPIVersion,
			ScenarioKind:         contract.ScenarioKind,
			ResultSchemaVersion:  contract.ResultSchemaVersion,
			CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		},
		Vocabularies: goldenVocabularies{
			ConfigurationKinds: []contract.ConfigurationKind{
				contract.ConfigurationKindValidating,
				contract.ConfigurationKindMutating,
			},
			EvaluationPhases: []contract.EvaluationPhase{
				contract.EvaluationPhaseSnapshotRouting,
			},
			Determinations: []contract.Determination{
				contract.DeterminationDeterminate,
				contract.DeterminationIndeterminate,
				contract.DeterminationUnsupported,
			},
			Outcomes: []contract.Outcome{
				contract.OutcomeCalled,
				contract.OutcomeSkipped,
				contract.OutcomeRejectedBeforeCall,
			},
			TraceResults: []contract.TraceResult{
				contract.TraceResultMatch,
				contract.TraceResultNoMatch,
				contract.TraceResultTrue,
				contract.TraceResultFalse,
				contract.TraceResultError,
				contract.TraceResultIndeterminate,
				contract.TraceResultUnsupported,
				contract.TraceResultNotRun,
			},
			DiagnosticSeverities: []contract.DiagnosticSeverity{
				contract.DiagnosticSeverityInfo,
				contract.DiagnosticSeverityWarning,
				contract.DiagnosticSeverityError,
			},
			AuthorizationVerdicts: []contract.AuthorizationVerdict{
				contract.AuthorizationVerdictAllow,
				contract.AuthorizationVerdictDeny,
				contract.AuthorizationVerdictNoOpinion,
				contract.AuthorizationVerdictError,
			},
			FeatureGatePolicies: []contract.FeatureGatePolicy{
				contract.FeatureGatePolicyKubernetesDefaults,
			},
		},
		ConfigurationVariants: []contract.WebhookConfiguration{
			{Validating: validating},
			{Mutating: mutating},
		},
		Scenario: scenario,
		Result:   result,
	}
}
