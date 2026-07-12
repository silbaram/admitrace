package evaluation_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	"github.com/silbaram/admitrace/internal/fixture"
	"github.com/silbaram/admitrace/internal/normalize"
	"github.com/silbaram/admitrace/internal/scenario"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestSnapshotFromScenarioPreservesConfigurationOrder(t *testing.T) {
	t.Parallel()

	for _, kind := range []contract.ConfigurationKind{
		contract.ConfigurationKindValidating,
		contract.ConfigurationKindMutating,
	} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			input := configuredScenario(kind, "first.example.com", "second.example.com")
			scenario.ApplyDefaults(&input)
			got, err := evaluation.SnapshotFromScenario(input)
			if err != nil {
				t.Fatalf("SnapshotFromScenario() error = %v", err)
			}
			if got.ConfigurationKind != kind || len(got.Webhooks) != 2 {
				t.Fatalf("snapshot kind/length = (%q, %d), want (%q, 2)", got.ConfigurationKind, len(got.Webhooks), kind)
			}
			if got.Webhooks[0].Name != "first.example.com" || got.Webhooks[1].Name != "second.example.com" {
				t.Errorf("webhook order = (%q, %q), want input order", got.Webhooks[0].Name, got.Webhooks[1].Name)
			}
		})
	}
}

func TestEvaluatePreservesWebhookOrderAndSnapshotIndependence(t *testing.T) {
	t.Parallel()

	for _, kind := range []contract.ConfigurationKind{
		contract.ConfigurationKindValidating,
		contract.ConfigurationKindMutating,
	} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			webhooks := []normalize.NormalizedWebhook{
				testWebhook(kind, 0, "first.example.com", "false"),
				testWebhook(kind, 1, "second.example.com", "true"),
				testWebhook(kind, 2, "third.example.com", "false"),
			}

			got := evaluation.NewEvaluator().Evaluate(context.Background(), testSnapshot(t, kind, webhooks))
			wantNames := []string{"first.example.com", "second.example.com", "third.example.com"}
			wantOutcomes := []contract.Outcome{contract.OutcomeSkipped, contract.OutcomeCalled, contract.OutcomeSkipped}
			wantPhase := contract.EvaluationPhaseSnapshotRouting
			if kind == contract.ConfigurationKindMutating {
				wantPhase = contract.EvaluationPhaseMutatingInitialSnapshot
			}
			if got.EvaluationPhase != wantPhase {
				t.Errorf("EvaluationPhase = %q, want %q", got.EvaluationPhase, wantPhase)
			}
			for i := range got.Webhooks {
				webhook := got.Webhooks[i]
				if webhook.WebhookName != wantNames[i] || webhook.WebhookIndex != i {
					t.Errorf("webhook[%d] identity = (%q, %d), want (%q, %d)", i, webhook.WebhookName, webhook.WebhookIndex, wantNames[i], i)
				}
				assertOutcome(t, webhook, contract.DeterminationDeterminate, &wantOutcomes[i])
				assertTrace(t, webhook.Trace)
			}
			if kind == contract.ConfigurationKindMutating {
				assertCapabilityDiagnostic(t, got.Diagnostics, "mutating patch chain and reinvocation")
			} else if len(got.Diagnostics) != 0 {
				t.Errorf("validating result diagnostics = %#v, want none", got.Diagnostics)
			}
			if err := got.Validate(); err != nil {
				t.Errorf("Validate() error = %v", err)
			}
		})
	}
}

func TestEvaluateContrastsSelectorErrorAndMissingNamespace(t *testing.T) {
	t.Parallel()

	for _, policy := range []admissionregistrationv1.FailurePolicyType{
		admissionregistrationv1.Fail,
		admissionregistrationv1.Ignore,
	} {
		t.Run(string(policy), func(t *testing.T) {
			t.Parallel()
			webhook := testWebhook(contract.ConfigurationKindValidating, 0, "selector.example.com", "true")
			webhook.FailurePolicy = policy
			webhook.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"environment": "production"}}

			t.Run("actual lookup error rejects before call", func(t *testing.T) {
				snapshot := testSnapshot(t, contract.ConfigurationKindValidating, []normalize.NormalizedWebhook{webhook})
				provider, err := fixture.NewNamespaceProvider(fixture.NamespaceEntry{
					Name: "demo",
					Err:  errors.New("fixture lookup failed"),
				})
				if err != nil {
					t.Fatalf("NewNamespaceProvider() error = %v", err)
				}
				snapshot.Namespaces = provider

				got := evaluation.NewEvaluator().Evaluate(context.Background(), snapshot).Webhooks[0]
				want := contract.OutcomeRejectedBeforeCall
				assertOutcome(t, got, contract.DeterminationDeterminate, &want)
				assertTerminal(t, got.Trace, contract.TraceResultError, contract.ReasonCodeKubernetesEvaluationError)
				if got.Diagnostics[0].Code != contract.ReasonCodeKubernetesEvaluationError {
					t.Errorf("diagnostic code = %q, want %q", got.Diagnostics[0].Code, contract.ReasonCodeKubernetesEvaluationError)
				}
			})

			t.Run("missing fixture remains indeterminate", func(t *testing.T) {
				got := evaluation.NewEvaluator().Evaluate(
					context.Background(),
					testSnapshot(t, contract.ConfigurationKindValidating, []normalize.NormalizedWebhook{webhook}),
				).Webhooks[0]
				assertOutcome(t, got, contract.DeterminationIndeterminate, nil)
				assertTerminal(t, got.Trace, contract.TraceResultIndeterminate, contract.ReasonCodeNamespaceContextMissing)
				if len(got.Diagnostics) != 1 || got.Diagnostics[0].MissingContext == nil {
					t.Fatalf("diagnostics = %#v, want one missing-context diagnostic", got.Diagnostics)
				}
			})
		})
	}
}

func TestEvaluateDryRunSideEffectsIsUnsupported(t *testing.T) {
	t.Parallel()

	webhook := testWebhook(contract.ConfigurationKindValidating, 0, "side-effects.example.com", "true")
	sideEffects := admissionregistrationv1.SideEffectClassSome
	webhook.SideEffects = &sideEffects
	webhook.AdmissionReviewVersions = []string{"v1"}
	snapshot := testSnapshot(t, contract.ConfigurationKindValidating, []normalize.NormalizedWebhook{webhook})
	snapshot.Request.DryRunPresent = true
	snapshot.Request.DryRun = true

	got := evaluation.NewEvaluator().Evaluate(context.Background(), snapshot).Webhooks[0]
	assertOutcome(t, got, contract.DeterminationUnsupported, nil)
	assertTerminal(t, got.Trace, contract.TraceResultUnsupported, contract.ReasonCodeCapabilityOutsideProfile)
	assertCapabilityDiagnostic(t, got.Diagnostics, "dry-run webhook invocation with side effects")
	if err := got.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestEvaluateCalledIsEligibilityNotTransportClaim(t *testing.T) {
	t.Parallel()

	webhook := testWebhook(contract.ConfigurationKindValidating, 0, "transport.example.com", "true")
	webhook.AdmissionReviewVersions = []string{"v1", "v1beta1"}
	got := evaluation.NewEvaluator().Evaluate(
		context.Background(),
		testSnapshot(t, contract.ConfigurationKindValidating, []normalize.NormalizedWebhook{webhook}),
	).Webhooks[0]
	want := contract.OutcomeCalled
	assertOutcome(t, got, contract.DeterminationDeterminate, &want)
	assertCapabilityDiagnostic(t, got.Diagnostics, "AdmissionReview negotiation and webhook transport")
	if got.Diagnostics[0].UnsupportedCapability.Detail != "no network request was made" {
		t.Errorf("transport detail = %q, want explicit no-network statement", got.Diagnostics[0].UnsupportedCapability.Detail)
	}
}

func testSnapshot(
	t *testing.T,
	kind contract.ConfigurationKind,
	webhooks []normalize.NormalizedWebhook,
) evaluation.Snapshot {
	t.Helper()
	namespaces, err := fixture.NewNamespaceProvider()
	if err != nil {
		t.Fatalf("NewNamespaceProvider() error = %v", err)
	}
	authorizer, err := fixture.NewAuthorizer(nil)
	if err != nil {
		t.Fatalf("NewAuthorizer() error = %v", err)
	}
	return evaluation.Snapshot{
		ScenarioID:           "snapshot",
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		ConfigurationKind:    kind,
		Webhooks:             webhooks,
		Request:              testRequest(),
		Namespaces:           namespaces,
		Authorizer:           authorizer,
	}
}

func testWebhook(
	kind contract.ConfigurationKind,
	index int,
	name string,
	expression string,
) normalize.NormalizedWebhook {
	fail := admissionregistrationv1.Fail
	none := admissionregistrationv1.SideEffectClassNone
	basePath := ".configuration.validatingWebhookConfiguration.webhooks"
	if kind == contract.ConfigurationKindMutating {
		basePath = ".configuration.mutatingWebhookConfiguration.webhooks"
	}
	return normalize.NormalizedWebhook{
		ConfigurationKind: kind,
		Name:              name,
		InputIndex:        index,
		SourcePath:        basePath + "[" + strconv.Itoa(index) + "]",
		Rules: []normalize.Rule{
			{
				Operations:  []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1"},
				Resources:   []string{"deployments"},
				Scope:       admissionregistrationv1.NamespacedScope,
			},
		},
		MatchPolicy: admissionregistrationv1.Exact,
		MatchConditions: []admissionregistrationv1.MatchCondition{
			{Name: "condition", Expression: expression},
		},
		FailurePolicy:           fail,
		SideEffects:             &none,
		AdmissionReviewVersions: []string{"v1"},
	}
}

func testRequest() normalize.RequestContext {
	return normalize.RequestContext{
		Operation:       admissionv1.Create,
		Kind:            schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		Resource:        schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		RequestKind:     schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		RequestResource: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		Scope:           contract.RequestScopeNamespaced,
		Namespace:       "demo",
		Name:            "sample",
		Object: normalize.ObjectSnapshot{
			State:     normalize.ObjectSnapshotObject,
			Raw:       []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"sample","namespace":"demo"}}`),
			GVK:       schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
			Name:      "sample",
			Namespace: "demo",
		},
		OldObject: normalize.ObjectSnapshot{State: normalize.ObjectSnapshotAbsent},
	}
}

func configuredScenario(kind contract.ConfigurationKind, names ...string) contract.Scenario {
	input := contract.Scenario{
		Metadata:             contract.ScenarioMetadata{Name: "configured"},
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		Request: contract.AdmissionRequest{
			Operation: admissionv1.Create,
			Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
			Resource:  metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			Scope:     contract.RequestScopeNamespaced,
			Namespace: "demo",
			Name:      "sample",
			Object:    []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"sample","namespace":"demo"}}`),
		},
	}
	rules := []admissionregistrationv1.RuleWithOperations{
		{
			Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
			Rule: admissionregistrationv1.Rule{
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1"},
				Resources:   []string{"deployments"},
			},
		},
	}
	none := admissionregistrationv1.SideEffectClassNone
	if kind == contract.ConfigurationKindValidating {
		configuration := &kube136.ValidatingWebhookConfiguration{}
		for _, name := range names {
			configuration.Webhooks = append(configuration.Webhooks, admissionregistrationv1.ValidatingWebhook{
				Name:                    name,
				Rules:                   rules,
				SideEffects:             &none,
				AdmissionReviewVersions: []string{"v1"},
			})
		}
		input.Configuration.Validating = configuration
		return input
	}
	configuration := &kube136.MutatingWebhookConfiguration{}
	for _, name := range names {
		configuration.Webhooks = append(configuration.Webhooks, admissionregistrationv1.MutatingWebhook{
			Name:                    name,
			Rules:                   rules,
			SideEffects:             &none,
			AdmissionReviewVersions: []string{"v1"},
		})
	}
	input.Configuration.Mutating = configuration
	return input
}

func assertOutcome(
	t *testing.T,
	got contract.WebhookEvaluation,
	wantDetermination contract.Determination,
	wantOutcome *contract.Outcome,
) {
	t.Helper()
	if got.Determination != wantDetermination || !reflect.DeepEqual(got.Outcome, wantOutcome) {
		t.Errorf("determination/outcome = (%q, %#v), want (%q, %#v)", got.Determination, got.Outcome, wantDetermination, wantOutcome)
	}
}

func assertTrace(t *testing.T, trace []contract.TraceStep) {
	t.Helper()
	terminalCount := 0
	for i := range trace {
		if trace[i].Sequence != i {
			t.Errorf("trace[%d].Sequence = %d, want %d", i, trace[i].Sequence, i)
		}
		if trace[i].Terminal {
			terminalCount++
		}
		if err := trace[i].Validate(); err != nil {
			t.Errorf("trace[%d].Validate() error = %v", i, err)
		}
	}
	if terminalCount != 1 || len(trace) == 0 || !trace[len(trace)-1].Terminal {
		t.Errorf("terminal trace count/position = (%d, %t), want one terminal last", terminalCount, len(trace) > 0 && trace[len(trace)-1].Terminal)
	}
}

func assertTerminal(
	t *testing.T,
	trace []contract.TraceStep,
	wantResult contract.TraceResult,
	wantReason contract.ReasonCode,
) {
	t.Helper()
	assertTrace(t, trace)
	terminal := trace[len(trace)-1]
	if terminal.Result != wantResult || terminal.ReasonCode != wantReason {
		t.Errorf("terminal = (%q, %q), want (%q, %q)", terminal.Result, terminal.ReasonCode, wantResult, wantReason)
	}
}

func assertCapabilityDiagnostic(t *testing.T, diagnostics []contract.Diagnostic, capability string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.UnsupportedCapability != nil && diagnostic.UnsupportedCapability.Capability == capability {
			return
		}
	}
	t.Errorf("diagnostics = %#v, want capability %q", diagnostics, capability)
}
