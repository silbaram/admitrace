package routing_test

import (
	"errors"
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	"github.com/silbaram/admitrace/internal/normalize"
	"github.com/silbaram/admitrace/internal/routing"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestMatchNamespaceSelectorClassifiesKubernetesBranches(t *testing.T) {
	t.Parallel()

	failure := errors.New("recorded lookup failure")
	provider := selectorNamespaceProvider(t,
		fixture.NamespaceEntry{Name: "team-prod", Namespace: selectorNamespace("team-prod", "prod")},
		fixture.NamespaceEntry{Name: "team-dev", Namespace: selectorNamespace("team-dev", "dev")},
		fixture.NamespaceEntry{Name: "faulty", Err: failure},
	)
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"environment": "prod"}}
	tests := []struct {
		name              string
		webhook           normalize.NormalizedWebhook
		request           normalize.RequestContext
		wantOutcome       routing.SelectorOutcome
		wantReason        contract.ReasonCode
		wantMatched       bool
		wantMode          kube136.NamespaceContextMode
		wantContextSource fixture.NamespaceContextSource
		wantError         error
	}{
		{
			name:    "Namespace CREATE uses request object labels",
			webhook: selectorWebhook(selector, nil),
			request: normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
				Scope:     contract.RequestScopeCluster,
				Object: normalize.ObjectSnapshot{
					State:  normalize.ObjectSnapshotObject,
					Raw:    []byte(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"team-prod","labels":{"environment":"prod"}}}`),
					Labels: map[string]string{"environment": "prod"},
				},
			},
			wantOutcome:       routing.SelectorOutcomeMatch,
			wantReason:        contract.ReasonCodeNamespaceSelectorMatch,
			wantMatched:       true,
			wantMode:          kube136.NamespaceContextModeRequestObject,
			wantContextSource: fixture.NamespaceContextRequestObject,
		},
		{
			name:    "namespaced request known no-match",
			webhook: selectorWebhook(selector, nil),
			request: normalize.RequestContext{
				Operation: admissionv1.Update,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "pods"},
				Scope:     contract.RequestScopeNamespaced,
				Namespace: "team-dev",
			},
			wantOutcome:       routing.SelectorOutcomeNoMatch,
			wantReason:        contract.ReasonCodeNamespaceSelectorNoMatch,
			wantMode:          kube136.NamespaceContextModeFixture,
			wantContextSource: fixture.NamespaceContextFixture,
		},
		{
			name:    "cluster scoped non-Namespace bypasses invalid selector and fixture",
			webhook: selectorWebhook(routingInvalidSelector(), nil),
			request: normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "nodes"},
				Scope:     contract.RequestScopeCluster,
				Namespace: "faulty",
			},
			wantOutcome:       routing.SelectorOutcomeMatch,
			wantReason:        contract.ReasonCodeNamespaceSelectorMatch,
			wantMatched:       true,
			wantMode:          kube136.NamespaceContextModeNotRequired,
			wantContextSource: fixture.NamespaceContextNotRequired,
		},
		{
			name:    "omitted selector defaults to match without missing fixture",
			webhook: selectorWebhook(nil, nil),
			request: normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "pods"},
				Scope:     contract.RequestScopeNamespaced,
				Namespace: "missing",
			},
			wantOutcome: routing.SelectorOutcomeMatch,
			wantReason:  contract.ReasonCodeNamespaceSelectorMatch,
			wantMatched: true,
			wantMode:    kube136.NamespaceContextModeFixture,
		},
		{
			name:    "missing namespace fixture remains missing context",
			webhook: selectorWebhook(selector, nil),
			request: normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "pods"},
				Scope:     contract.RequestScopeNamespaced,
				Namespace: "missing",
			},
			wantOutcome: routing.SelectorOutcomeMissingContext,
			wantReason:  contract.ReasonCodeNamespaceContextMissing,
			wantMode:    kube136.NamespaceContextModeFixture,
			wantError:   contract.ErrMissingContext,
		},
		{
			name:    "recorded fixture failure is an evaluation error",
			webhook: selectorWebhook(selector, nil),
			request: normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "pods"},
				Scope:     contract.RequestScopeNamespaced,
				Namespace: "faulty",
			},
			wantOutcome: routing.SelectorOutcomeEvaluationError,
			wantReason:  contract.ReasonCodeKubernetesEvaluationError,
			wantMode:    kube136.NamespaceContextModeFixture,
			wantError:   contract.ErrKubernetesEvaluation,
		},
		{
			name:    "invalid selector is an evaluation error before missing fixture",
			webhook: selectorWebhook(routingInvalidSelector(), nil),
			request: normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "pods"},
				Scope:     contract.RequestScopeNamespaced,
				Namespace: "missing",
			},
			wantOutcome: routing.SelectorOutcomeEvaluationError,
			wantReason:  contract.ReasonCodeKubernetesEvaluationError,
			wantMode:    kube136.NamespaceContextModeFixture,
			wantError:   contract.ErrKubernetesEvaluation,
		},
		{
			name:    "Namespace CREATE null object is an evaluation error",
			webhook: selectorWebhook(selector, nil),
			request: normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
				Scope:     contract.RequestScopeCluster,
				Object:    normalize.ObjectSnapshot{State: normalize.ObjectSnapshotNull},
			},
			wantOutcome: routing.SelectorOutcomeEvaluationError,
			wantReason:  contract.ReasonCodeKubernetesEvaluationError,
			wantMode:    kube136.NamespaceContextModeRequestObject,
			wantError:   contract.ErrKubernetesEvaluation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := routing.MatchNamespaceSelector(test.webhook, test.request, provider)
			assertSelectorResult(t, got.SelectorResult, test.wantOutcome, test.wantReason, test.wantMatched, ".webhooks[3].namespaceSelector", test.wantError)
			if got.ContextMode != test.wantMode {
				t.Errorf("ContextMode = %q, want %q", got.ContextMode, test.wantMode)
			}
			if got.ContextSource != test.wantContextSource {
				t.Errorf("ContextSource = %q, want %q", got.ContextSource, test.wantContextSource)
			}
		})
	}
}

func TestMatchObjectSelectorPreservesPayloadStatesAndOR(t *testing.T) {
	t.Parallel()

	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"selected": "true"}}
	tests := []struct {
		name        string
		webhook     normalize.NormalizedWebhook
		request     normalize.RequestContext
		wantOutcome routing.SelectorOutcome
		wantReason  contract.ReasonCode
		wantMatched bool
		wantError   error
	}{
		{
			name:    "CREATE matches object and preserves absent oldObject",
			webhook: selectorWebhook(nil, selector),
			request: normalize.RequestContext{
				Operation: admissionv1.Create,
				Object: normalize.ObjectSnapshot{
					State:  normalize.ObjectSnapshotObject,
					Labels: map[string]string{"selected": "true"},
				},
				OldObject: normalize.ObjectSnapshot{State: normalize.ObjectSnapshotAbsent},
			},
			wantOutcome: routing.SelectorOutcomeMatch,
			wantReason:  contract.ReasonCodeObjectSelectorMatch,
			wantMatched: true,
		},
		{
			name:    "DELETE matches oldObject and preserves null object",
			webhook: selectorWebhook(nil, selector),
			request: normalize.RequestContext{
				Operation: admissionv1.Delete,
				Object:    normalize.ObjectSnapshot{State: normalize.ObjectSnapshotNull},
				OldObject: normalize.ObjectSnapshot{
					State:  normalize.ObjectSnapshotObject,
					Labels: map[string]string{"selected": "true"},
				},
			},
			wantOutcome: routing.SelectorOutcomeMatch,
			wantReason:  contract.ReasonCodeObjectSelectorMatch,
			wantMatched: true,
		},
		{
			name:    "UPDATE matches oldObject when object does not match",
			webhook: selectorWebhook(nil, selector),
			request: normalize.RequestContext{
				Operation: admissionv1.Update,
				Object: normalize.ObjectSnapshot{
					State:  normalize.ObjectSnapshotObject,
					Labels: map[string]string{"selected": "false"},
				},
				OldObject: normalize.ObjectSnapshot{
					State:  normalize.ObjectSnapshotObject,
					Labels: map[string]string{"selected": "true"},
				},
			},
			wantOutcome: routing.SelectorOutcomeMatch,
			wantReason:  contract.ReasonCodeObjectSelectorMatch,
			wantMatched: true,
		},
		{
			name:    "known no-match is determinate",
			webhook: selectorWebhook(nil, selector),
			request: normalize.RequestContext{
				Object: normalize.ObjectSnapshot{
					State:  normalize.ObjectSnapshotObject,
					Labels: map[string]string{"selected": "false"},
				},
				OldObject: normalize.ObjectSnapshot{State: normalize.ObjectSnapshotNull},
			},
			wantOutcome: routing.SelectorOutcomeNoMatch,
			wantReason:  contract.ReasonCodeObjectSelectorNoMatch,
		},
		{
			name:    "omitted selector defaults to match and preserves absent and null",
			webhook: selectorWebhook(nil, nil),
			request: normalize.RequestContext{
				Object:    normalize.ObjectSnapshot{State: normalize.ObjectSnapshotAbsent},
				OldObject: normalize.ObjectSnapshot{State: normalize.ObjectSnapshotNull},
			},
			wantOutcome: routing.SelectorOutcomeMatch,
			wantReason:  contract.ReasonCodeObjectSelectorMatch,
			wantMatched: true,
		},
		{
			name:    "invalid selector is a Kubernetes evaluation error",
			webhook: selectorWebhook(nil, routingInvalidSelector()),
			request: normalize.RequestContext{
				Object:    normalize.ObjectSnapshot{State: normalize.ObjectSnapshotAbsent},
				OldObject: normalize.ObjectSnapshot{State: normalize.ObjectSnapshotNull},
			},
			wantOutcome: routing.SelectorOutcomeEvaluationError,
			wantReason:  contract.ReasonCodeKubernetesEvaluationError,
			wantError:   contract.ErrKubernetesEvaluation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := routing.MatchObjectSelector(test.webhook, test.request)
			assertSelectorResult(t, got.SelectorResult, test.wantOutcome, test.wantReason, test.wantMatched, ".webhooks[3].objectSelector", test.wantError)
			if got.ObjectState != test.request.Object.State {
				t.Errorf("ObjectState = %q, want %q", got.ObjectState, test.request.Object.State)
			}
			if got.OldObjectState != test.request.OldObject.State {
				t.Errorf("OldObjectState = %q, want %q", got.OldObjectState, test.request.OldObject.State)
			}
		})
	}
}

func assertSelectorResult(
	t *testing.T,
	got routing.SelectorResult,
	wantOutcome routing.SelectorOutcome,
	wantReason contract.ReasonCode,
	wantMatched bool,
	wantPath string,
	wantError error,
) {
	t.Helper()

	if got.Outcome != wantOutcome {
		t.Errorf("Outcome = %q, want %q", got.Outcome, wantOutcome)
	}
	if got.Matched != wantMatched {
		t.Errorf("Matched = %t, want %t", got.Matched, wantMatched)
	}
	if got.ReasonCode != wantReason {
		t.Errorf("ReasonCode = %q, want %q", got.ReasonCode, wantReason)
	}
	if got.SourcePath != wantPath {
		t.Errorf("SourcePath = %q, want %q", got.SourcePath, wantPath)
	}
	if wantError == nil && got.Err != nil {
		t.Errorf("Err = %v, want nil", got.Err)
	}
	if wantError != nil && !errors.Is(got.Err, wantError) {
		t.Errorf("Err = %v, want category %v", got.Err, wantError)
	}
}

func selectorNamespaceProvider(t *testing.T, entries ...fixture.NamespaceEntry) fixture.NamespaceProvider {
	t.Helper()

	provider, err := fixture.NewNamespaceProvider(entries...)
	if err != nil {
		t.Fatalf("NewNamespaceProvider() error = %v", err)
	}
	return provider
}

func selectorNamespace(name, environment string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   name,
		Labels: map[string]string{"environment": environment},
	}}
}

func selectorWebhook(namespaceSelector, objectSelector *metav1.LabelSelector) normalize.NormalizedWebhook {
	return normalize.NormalizedWebhook{
		SourcePath:        ".webhooks[3]",
		NamespaceSelector: namespaceSelector,
		ObjectSelector:    objectSelector,
	}
}

func routingInvalidSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
		Key:      "selected",
		Operator: metav1.LabelSelectorOperator("Between"),
	}}}
}
