package routing_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	"github.com/silbaram/admitrace/internal/normalize"
	"github.com/silbaram/admitrace/internal/routing"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type recordingEquivalenceLookup struct {
	calls      int
	candidates []contract.EquivalentResource
	err        error
}

func (lookup *recordingEquivalenceLookup) Lookup(contract.ResourceReference) ([]contract.EquivalentResource, error) {
	lookup.calls++
	return append([]contract.EquivalentResource(nil), lookup.candidates...), lookup.err
}

func TestShouldCallHookPreCELExactMatchDoesNotLookupEquivalence(t *testing.T) {
	t.Parallel()

	lookup := &recordingEquivalenceLookup{err: errors.New("must not be called")}
	got := routing.ShouldCallHookPreCEL(
		preCELWebhook(admissionregistrationv1.Equivalent, preCELRule("apps", "v1", "deployments")),
		preCELRequest("apps", "v1", "Deployment", "deployments"),
		emptyNamespaceProvider(t),
		lookup,
	)

	if got.Status != routing.PreCELStatusContinue || got.Continuation == nil {
		t.Fatalf("ShouldCallHookPreCEL() status/continuation = (%q, %#v), want continue", got.Status, got.Continuation)
	}
	if lookup.calls != 0 || got.Equivalent != nil {
		t.Errorf("equivalence lookup/result = (%d, %#v), want not evaluated", lookup.calls, got.Equivalent)
	}
	assertTraceStages(t, got.Trace, "namespaceSelector", "objectSelector", "exactRules")
	assertValidTrace(t, got.Trace)
}

func TestShouldCallHookPreCELExactPolicyMissDoesNotLookupEquivalence(t *testing.T) {
	t.Parallel()

	lookup := &recordingEquivalenceLookup{err: errors.New("must not be called")}
	got := routing.ShouldCallHookPreCEL(
		preCELWebhook(admissionregistrationv1.Exact, preCELRule("batch", "v1", "jobs")),
		preCELRequest("apps", "v1", "Deployment", "deployments"),
		emptyNamespaceProvider(t),
		lookup,
	)

	if got.Status != routing.PreCELStatusSkipped || got.Outcome == nil || *got.Outcome != contract.OutcomeSkipped {
		t.Fatalf("status/outcome = (%q, %#v), want skipped", got.Status, got.Outcome)
	}
	if lookup.calls != 0 || got.Equivalent != nil {
		t.Errorf("equivalence lookup/result = (%d, %#v), want not evaluated", lookup.calls, got.Equivalent)
	}
	assertTraceStages(t, got.Trace, "namespaceSelector", "objectSelector", "exactRules")
	if !got.Trace[2].Terminal || got.Trace[2].ReasonCode != contract.ReasonCodeRuleNoMatch {
		t.Errorf("exact trace = %#v, want terminal no-match", got.Trace[2])
	}
	assertValidTrace(t, got.Trace)
}

func TestShouldCallHookPreCELBuffersAndResolvesSelectorProblems(t *testing.T) {
	t.Parallel()

	lookupFailure := errors.New("namespace lookup failure")
	namespaces, err := fixture.NewNamespaceProvider(fixture.NamespaceEntry{Name: "faulty", Err: lookupFailure})
	if err != nil {
		t.Fatalf("NewNamespaceProvider() error = %v", err)
	}
	invalidSelector := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "x", Operator: "Invalid"}}}
	webhook := preCELWebhook(admissionregistrationv1.Exact, preCELRule("apps", "v1", "deployments"))
	webhook.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"team": "platform"}}
	webhook.ObjectSelector = invalidSelector
	request := preCELRequest("apps", "v1", "Deployment", "deployments")
	request.Scope = contract.RequestScopeNamespaced
	request.Namespace = "faulty"

	t.Run("applicable rule surfaces namespace problem first", func(t *testing.T) {
		got := routing.ShouldCallHookPreCEL(webhook, request, namespaces, nil)
		if got.Status != routing.PreCELStatusProblem || !errors.Is(got.Err, contract.ErrKubernetesEvaluation) {
			t.Fatalf("status/error = (%q, %v), want namespace evaluation problem", got.Status, got.Err)
		}
		assertTraceStages(t, got.Trace, "namespaceSelector", "objectSelector", "exactRules", "namespaceSelectorProblem")
		if !got.Trace[0].Pending || got.Trace[0].ReasonCode != contract.ReasonCodeEvaluationProblemPending {
			t.Errorf("namespace trace = %#v, want original pending problem", got.Trace[0])
		}
		if !got.Trace[1].Discarded || got.Trace[1].ReasonCode != contract.ReasonCodeEvaluationProblemDiscarded {
			t.Errorf("object trace = %#v, want discarded later problem", got.Trace[1])
		}
		if !got.Trace[3].Terminal || got.Trace[3].ReasonCode != contract.ReasonCodeKubernetesEvaluationError {
			t.Errorf("surface trace = %#v, want terminal namespace problem", got.Trace[3])
		}
		assertValidTrace(t, got.Trace)
	})

	t.Run("no applicable rule discards both problems", func(t *testing.T) {
		noRules := webhook
		noRules.Rules = nil
		got := routing.ShouldCallHookPreCEL(noRules, request, namespaces, nil)
		if got.Status != routing.PreCELStatusSkipped || got.Outcome == nil || *got.Outcome != contract.OutcomeSkipped {
			t.Fatalf("status/outcome = (%q, %#v), want skipped", got.Status, got.Outcome)
		}
		for i := 0; i < 2; i++ {
			if !got.Trace[i].Discarded || got.Trace[i].ReasonCode != contract.ReasonCodeEvaluationProblemDiscarded {
				t.Errorf("trace[%d] = %#v, want discarded selector problem", i, got.Trace[i])
			}
		}
		if !got.Trace[2].Terminal || got.Trace[2].ReasonCode != contract.ReasonCodeRuleNoMatch {
			t.Errorf("exact trace = %#v, want terminal no-match", got.Trace[2])
		}
		assertValidTrace(t, got.Trace)
	})

	t.Run("applicable rule surfaces missing namespace context", func(t *testing.T) {
		missingNamespaces := emptyNamespaceProvider(t)
		missingWebhook := webhook
		missingWebhook.ObjectSelector = nil
		missingRequest := request
		missingRequest.Namespace = "missing"

		got := routing.ShouldCallHookPreCEL(missingWebhook, missingRequest, missingNamespaces, nil)
		if got.Status != routing.PreCELStatusProblem || !errors.Is(got.Err, contract.ErrMissingContext) {
			t.Fatalf("status/error = (%q, %v), want missing context problem", got.Status, got.Err)
		}
		assertTraceStages(t, got.Trace, "namespaceSelector", "objectSelector", "exactRules", "namespaceSelectorProblem")
		if got.Trace[3].Result != contract.TraceResultIndeterminate || got.Trace[3].ReasonCode != contract.ReasonCodeNamespaceContextMissing || !got.Trace[3].Terminal {
			t.Errorf("surface trace = %#v, want terminal missing namespace context", got.Trace[3])
		}
		assertValidTrace(t, got.Trace)
	})
}

func TestShouldCallHookPreCELEquivalentOrderAndRequestedResourceSkip(t *testing.T) {
	t.Parallel()

	lookup := &recordingEquivalenceLookup{candidates: []contract.EquivalentResource{
		preCELEquivalent("apps", "v1", "deployments", "apps", "v1", "Deployment"),
		preCELEquivalent("apps", "v1beta1", "deployments", "apps", "v1beta1", "Deployment"),
		preCELEquivalent("extensions", "v1beta1", "deployments", "extensions", "v1beta1", "Deployment"),
	}}
	webhook := preCELWebhook(
		admissionregistrationv1.Equivalent,
		preCELRule("extensions", "v1beta1", "deployments"),
		preCELRule("apps", "v1beta1", "deployments"),
	)
	got := routing.ShouldCallHookPreCEL(
		webhook,
		preCELRequest("apps", "v1", "Deployment", "deployments"),
		emptyNamespaceProvider(t),
		lookup,
	)

	if got.Status != routing.PreCELStatusContinue || got.Continuation == nil {
		t.Fatalf("status/continuation = (%q, %#v), want continue", got.Status, got.Continuation)
	}
	if lookup.calls != 1 || got.Equivalent == nil || !got.Equivalent.LookupPerformed {
		t.Fatalf("lookup/result = (%d, %#v), want one lookup", lookup.calls, got.Equivalent)
	}
	invocation := got.Continuation.Invocation
	if invocation.Resource.Group != "extensions" || invocation.MatchedRuleIndex != 0 || !invocation.Equivalent {
		t.Errorf("invocation = %#v, want first rule with extensions equivalent", invocation)
	}
	wantEvaluations := []routing.EquivalentRuleEvaluation{
		{RuleIndex: 0, EquivalentIndex: 1, Resource: schema.GroupVersionResource{Group: "apps", Version: "v1beta1", Resource: "deployments"}},
		{RuleIndex: 0, EquivalentIndex: 2, Resource: schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "deployments"}, Matched: true},
	}
	if !reflect.DeepEqual(got.Equivalent.Evaluations, wantEvaluations) {
		t.Errorf("Evaluations = %#v, want rules-outer order %#v", got.Equivalent.Evaluations, wantEvaluations)
	}
	assertTraceStages(t, got.Trace, "namespaceSelector", "objectSelector", "exactRules", "equivalentRules")
	assertValidTrace(t, got.Trace)
}

func TestShouldCallHookPreCELClassifiesEquivalenceLookupProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantResult contract.TraceResult
		wantReason contract.ReasonCode
		wantError  error
	}{
		{
			name: "missing fixture",
			err: &contract.MissingContextError{
				Context: "equivalence",
				Err:     errors.New("fixture is required"),
			},
			wantResult: contract.TraceResultIndeterminate,
			wantReason: contract.ReasonCodeEquivalenceContextMissing,
			wantError:  contract.ErrMissingContext,
		},
		{
			name: "unsupported mapping",
			err: &contract.UnsupportedCapabilityError{
				Capability: "cross-subresource equivalence",
				Err:        errors.New("mapping is outside the profile"),
			},
			wantResult: contract.TraceResultUnsupported,
			wantReason: contract.ReasonCodeCapabilityOutsideProfile,
			wantError:  contract.ErrUnsupportedCapability,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			lookup := &recordingEquivalenceLookup{err: test.err}
			got := routing.ShouldCallHookPreCEL(
				preCELWebhook(admissionregistrationv1.Equivalent, preCELRule("batch", "v1", "jobs")),
				preCELRequest("apps", "v1", "Deployment", "deployments"),
				emptyNamespaceProvider(t),
				lookup,
			)

			if got.Status != routing.PreCELStatusProblem || !errors.Is(got.Err, test.wantError) {
				t.Fatalf("status/error = (%q, %v), want problem category %v", got.Status, got.Err, test.wantError)
			}
			if lookup.calls != 1 || got.Equivalent == nil || !got.Equivalent.LookupPerformed {
				t.Fatalf("lookup/result = (%d, %#v), want one attempted lookup", lookup.calls, got.Equivalent)
			}
			assertTraceStages(t, got.Trace, "namespaceSelector", "objectSelector", "exactRules", "equivalentRules")
			terminal := got.Trace[3]
			if !terminal.Terminal || terminal.Result != test.wantResult || terminal.ReasonCode != test.wantReason {
				t.Errorf("equivalent trace = %#v, want terminal %q/%q", terminal, test.wantResult, test.wantReason)
			}
			assertValidTrace(t, got.Trace)
		})
	}
}

func TestShouldCallHookPreCELShortCircuitsSelectorsAndSelfExemption(t *testing.T) {
	t.Parallel()

	t.Run("namespace no-match stops before object and rules", func(t *testing.T) {
		namespaces, err := fixture.NewNamespaceProvider(fixture.NamespaceEntry{
			Name: "dev",
			Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name:   "dev",
				Labels: map[string]string{"environment": "dev"},
			}},
		})
		if err != nil {
			t.Fatalf("NewNamespaceProvider() error = %v", err)
		}
		webhook := preCELWebhook(admissionregistrationv1.Equivalent, preCELRule("apps", "v1", "deployments"))
		webhook.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"environment": "prod"}}
		request := preCELRequest("apps", "v1", "Deployment", "deployments")
		request.Scope = contract.RequestScopeNamespaced
		request.Namespace = "dev"
		lookup := &recordingEquivalenceLookup{}

		got := routing.ShouldCallHookPreCEL(webhook, request, namespaces, lookup)
		assertTraceStages(t, got.Trace, "namespaceSelector")
		if lookup.calls != 0 || got.Status != routing.PreCELStatusSkipped || !got.Trace[0].Terminal {
			t.Errorf("lookup/status/trace = (%d, %q, %#v), want immediate skip", lookup.calls, got.Status, got.Trace)
		}
	})

	t.Run("self configuration precheck runs before selectors", func(t *testing.T) {
		request := preCELRequest("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "validatingwebhookconfigurations")
		got := routing.ShouldCallHookPreCEL(normalize.NormalizedWebhook{}, request, fixture.NamespaceProvider{}, nil)
		assertTraceStages(t, got.Trace, "admissionConfigurationExemption")
		if got.Status != routing.PreCELStatusSkipped || got.Trace[0].ReasonCode != contract.ReasonCodeAdmissionConfigurationExcluded || !got.Trace[0].Terminal {
			t.Errorf("result = %#v, want stable self-exclusion skip", got)
		}
		assertValidTrace(t, got.Trace)
	})
}

func preCELWebhook(policy admissionregistrationv1.MatchPolicyType, rules ...normalize.Rule) normalize.NormalizedWebhook {
	return normalize.NormalizedWebhook{
		SourcePath:  ".configuration.validatingWebhookConfiguration.webhooks[0]",
		MatchPolicy: policy,
		Rules:       rules,
	}
}

func preCELRule(group, version, resource string) normalize.Rule {
	return normalize.Rule{
		Operations:  []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
		APIGroups:   []string{group},
		APIVersions: []string{version},
		Resources:   []string{resource},
		Scope:       admissionregistrationv1.AllScopes,
	}
}

func preCELRequest(group, version, kind, resource string) normalize.RequestContext {
	return normalize.RequestContext{
		Operation: admissionv1.Create,
		Kind:      schema.GroupVersionKind{Group: group, Version: version, Kind: kind},
		Resource:  schema.GroupVersionResource{Group: group, Version: version, Resource: resource},
		Scope:     contract.RequestScopeCluster,
		Object:    normalize.ObjectSnapshot{State: normalize.ObjectSnapshotObject},
		OldObject: normalize.ObjectSnapshot{State: normalize.ObjectSnapshotAbsent},
	}
}

func preCELEquivalent(group, version, resource, kindGroup, kindVersion, kind string) contract.EquivalentResource {
	return contract.EquivalentResource{
		GVR: metav1.GroupVersionResource{Group: group, Version: version, Resource: resource},
		GVK: metav1.GroupVersionKind{Group: kindGroup, Version: kindVersion, Kind: kind},
	}
}

func emptyNamespaceProvider(t *testing.T) fixture.NamespaceProvider {
	t.Helper()
	provider, err := fixture.NewNamespaceProvider()
	if err != nil {
		t.Fatalf("NewNamespaceProvider() error = %v", err)
	}
	return provider
}

func assertTraceStages(t *testing.T, trace []contract.TraceStep, want ...string) {
	t.Helper()
	got := make([]string, len(trace))
	for i := range trace {
		got[i] = trace[i].Stage
		if trace[i].Sequence != i {
			t.Errorf("trace[%d].Sequence = %d, want %d", i, trace[i].Sequence, i)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("trace stages = %#v, want %#v", got, want)
	}
}

func assertValidTrace(t *testing.T, trace []contract.TraceStep) {
	t.Helper()
	for i := range trace {
		if err := trace[i].Validate(); err != nil {
			t.Errorf("trace[%d].Validate() error = %v", i, err)
		}
	}
}
