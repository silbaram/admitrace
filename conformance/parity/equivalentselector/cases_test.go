package equivalentselector

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/silbaram/admitrace/conformance/parity"
	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const groupName = "equivalent-selector"

type unsupportedEquivalence struct{}

func (unsupportedEquivalence) Lookup(contract.ResourceReference) ([]contract.EquivalentResource, error) {
	return nil, &contract.UnsupportedCapabilityError{
		Capability: "cross-subresource equivalence",
		Err:        errors.New("recorded fixture does not model status-to-scale conversion"),
	}
}

func TestEquivalentSelectorParity(t *testing.T) {
	testCases := cases()
	if err := parity.ValidateCases(groupName, testCases); err != nil {
		t.Fatal(err)
	}
	for _, row := range parity.CoverageReport(testCases) {
		t.Log(row)
	}

	for _, testCase := range testCases {
		t.Run(testCase.Scenario.Metadata.Name, func(t *testing.T) {
			actual, err := evaluate(t, testCase)
			if err != nil {
				t.Fatal(err)
			}
			if err := parity.CompareEvaluation(testCase, actual); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEquivalentSelectorCoverage(t *testing.T) {
	testCases := cases()
	if got, want := len(testCases), 7; got != want {
		t.Fatalf("Scenario count = %d, want %d", got, want)
	}
	wantTags := []string{
		"applicable-selector-error",
		"equivalent-explicit-mapping",
		"equivalent-mapping-missing",
		"equivalent-mapping-unsupported",
		"exact-first",
		"missing-fixture",
		"pending-selector-discarded",
	}
	var gotTags []string
	for _, testCase := range testCases {
		gotTags = append(gotTags, testCase.CoverageTags...)
	}
	for _, wantTag := range wantTags {
		if !slices.Contains(gotTags, wantTag) {
			t.Errorf("coverage tags missing %q", wantTag)
		}
	}
}

func evaluate(t *testing.T, testCase parity.Case) (contract.WebhookEvaluation, error) {
	t.Helper()
	switch testCase.Scenario.Metadata.Name {
	case "equivalent-mapping-unsupported":
		snapshot, err := parity.BuildSnapshot(testCase)
		if err != nil {
			return contract.WebhookEvaluation{}, err
		}
		snapshot.Equivalents = unsupportedEquivalence{}
		return parity.EvaluateSnapshot(context.Background(), testCase.Scenario.Metadata.Name, snapshot)
	case "selector-pending-error-discarded", "selector-applicable-error-rejected":
		snapshot, err := parity.BuildSnapshot(testCase)
		if err != nil {
			return contract.WebhookEvaluation{}, err
		}
		namespaces, err := fixture.NewNamespaceProvider(fixture.NamespaceEntry{
			Name: "default",
			Err:  errors.New("recorded namespace lookup failure"),
		})
		if err != nil {
			return contract.WebhookEvaluation{}, err
		}
		snapshot.Namespaces = namespaces
		return parity.EvaluateSnapshot(context.Background(), testCase.Scenario.Metadata.Name, snapshot)
	default:
		return parity.Evaluate(context.Background(), testCase)
	}
}

func cases() []parity.Case {
	called := contract.OutcomeCalled
	skipped := contract.OutcomeSkipped
	rejected := contract.OutcomeRejectedBeforeCall
	return []parity.Case{
		{
			Group:           groupName,
			Scenario:        exactFirstScenario(),
			OracleType:      parity.OracleGoldenTrace,
			OracleRationale: "the exact pass is locally replayed before any provided equivalence mapping is consulted",
			CoverageTags:    []string{"equivalent", "exact-first", "validating"},
			Expected: parity.ExpectedResult{
				Determination: contract.DeterminationDeterminate,
				Outcome:       &called,
				ReasonCode:    contract.ReasonCodeMatchConditionsTrue,
				Trace: []parity.TraceExpectation{
					trace("namespaceSelector", contract.TraceResultMatch, contract.ReasonCodeNamespaceSelectorMatch, false),
					trace("objectSelector", contract.TraceResultMatch, contract.ReasonCodeObjectSelectorMatch, false),
					trace("exactRules", contract.TraceResultMatch, contract.ReasonCodeRuleMatch, false),
					trace("matchConditions", contract.TraceResultTrue, contract.ReasonCodeMatchConditionsTrue, true),
				},
				Diagnostics: []contract.ReasonCode{contract.ReasonCodeCapabilityOutsideProfile},
			},
		},
		{
			Group:           groupName,
			Scenario:        explicitMappingScenario(),
			OracleType:      parity.OracleKubeAPIServerObservation,
			OracleRationale: "a two-version CRD observation verifies that Equivalent invokes only after the exact version misses",
			CoverageTags:    []string{"equivalent", "equivalent-explicit-mapping", "kube-apiserver", "validating"},
			Expected: parity.ExpectedResult{
				Determination: contract.DeterminationDeterminate,
				Outcome:       &called,
				ReasonCode:    contract.ReasonCodeMatchConditionsTrue,
				Diagnostics:   []contract.ReasonCode{contract.ReasonCodeCapabilityOutsideProfile},
			},
		},
		{
			Group:           groupName,
			Scenario:        equivalentScenario("equivalent-mapping-missing", nil),
			OracleType:      parity.OracleIncompleteContract,
			OracleRationale: "the API server discovers equivalence internally, while offline replay must stop when the explicit mapping fixture is absent",
			CoverageTags:    []string{"equivalent", "equivalent-mapping-missing", "missing-fixture"},
			Expected: parity.ExpectedResult{
				Determination: contract.DeterminationIndeterminate,
				ReasonCode:    contract.ReasonCodeEquivalenceContextMissing,
				Trace: []parity.TraceExpectation{
					trace("namespaceSelector", contract.TraceResultMatch, contract.ReasonCodeNamespaceSelectorMatch, false),
					trace("objectSelector", contract.TraceResultMatch, contract.ReasonCodeObjectSelectorMatch, false),
					trace("exactRules", contract.TraceResultNoMatch, contract.ReasonCodeRuleNoMatch, false),
					trace("equivalentRules", contract.TraceResultIndeterminate, contract.ReasonCodeEquivalenceContextMissing, true),
				},
				Diagnostics: []contract.ReasonCode{contract.ReasonCodeEquivalenceContextMissing},
			},
		},
		{
			Group:           groupName,
			Scenario:        equivalentScenario("equivalent-mapping-unsupported", emptyMapping()),
			OracleType:      parity.OracleIncompleteContract,
			OracleRationale: "cross-subresource conversion is outside the offline fixture contract and must never be guessed",
			CoverageTags:    []string{"equivalent", "equivalent-mapping-unsupported", "unsupported-mapping"},
			Expected: parity.ExpectedResult{
				Determination: contract.DeterminationUnsupported,
				ReasonCode:    contract.ReasonCodeCapabilityOutsideProfile,
				Trace: []parity.TraceExpectation{
					trace("namespaceSelector", contract.TraceResultMatch, contract.ReasonCodeNamespaceSelectorMatch, false),
					trace("objectSelector", contract.TraceResultMatch, contract.ReasonCodeObjectSelectorMatch, false),
					trace("exactRules", contract.TraceResultNoMatch, contract.ReasonCodeRuleNoMatch, false),
					trace("equivalentRules", contract.TraceResultUnsupported, contract.ReasonCodeCapabilityOutsideProfile, true),
				},
				Diagnostics: []contract.ReasonCode{contract.ReasonCodeCapabilityOutsideProfile},
			},
		},
		{
			Group:           groupName,
			Scenario:        selectorScenario("selector-pending-error-discarded", false),
			OracleType:      parity.OracleGoldenTrace,
			OracleRationale: "Kubernetes defers selector lookup errors and discards them when no rule applies",
			CoverageTags:    []string{"pending-selector-discarded", "rule-no-match", "selector"},
			Expected: parity.ExpectedResult{
				Determination: contract.DeterminationDeterminate,
				Outcome:       &skipped,
				ReasonCode:    contract.ReasonCodeRuleNoMatch,
				Trace: []parity.TraceExpectation{
					traceDiscarded("namespaceSelector", contract.TraceResultError),
					trace("objectSelector", contract.TraceResultMatch, contract.ReasonCodeObjectSelectorMatch, false),
					trace("exactRules", contract.TraceResultNoMatch, contract.ReasonCodeRuleNoMatch, true),
				},
			},
		},
		{
			Group:           groupName,
			Scenario:        selectorScenario("selector-applicable-error-rejected", true),
			OracleType:      parity.OracleGoldenTrace,
			OracleRationale: "an applicable rule makes the recorded selector evaluation error controlling before webhook transport",
			CoverageTags:    []string{"applicable-selector-error", "rejected-before-call", "selector"},
			Expected: parity.ExpectedResult{
				Determination: contract.DeterminationDeterminate,
				Outcome:       &rejected,
				ReasonCode:    contract.ReasonCodeKubernetesEvaluationError,
				Trace: []parity.TraceExpectation{
					tracePending("namespaceSelector", contract.TraceResultError),
					trace("objectSelector", contract.TraceResultMatch, contract.ReasonCodeObjectSelectorMatch, false),
					trace("exactRules", contract.TraceResultMatch, contract.ReasonCodeRuleMatch, false),
					trace("namespaceSelectorProblem", contract.TraceResultError, contract.ReasonCodeKubernetesEvaluationError, true),
				},
				Diagnostics: []contract.ReasonCode{contract.ReasonCodeKubernetesEvaluationError},
			},
		},
		{
			Group:           groupName,
			Scenario:        selectorScenario("selector-namespace-fixture-missing", true),
			OracleType:      parity.OracleIncompleteContract,
			OracleRationale: "a namespaced selector cannot be completed without the exact Namespace fixture",
			CoverageTags:    []string{"missing-fixture", "namespace-selector", "selector"},
			Expected: parity.ExpectedResult{
				Determination: contract.DeterminationIndeterminate,
				ReasonCode:    contract.ReasonCodeNamespaceContextMissing,
				Trace: []parity.TraceExpectation{
					tracePending("namespaceSelector", contract.TraceResultIndeterminate),
					trace("objectSelector", contract.TraceResultMatch, contract.ReasonCodeObjectSelectorMatch, false),
					trace("exactRules", contract.TraceResultMatch, contract.ReasonCodeRuleMatch, false),
					trace("namespaceSelectorProblem", contract.TraceResultIndeterminate, contract.ReasonCodeNamespaceContextMissing, true),
				},
				Diagnostics: []contract.ReasonCode{contract.ReasonCodeNamespaceContextMissing},
			},
		},
	}
}

func baseScenario(name, requestVersion, ruleVersion string) contract.Scenario {
	failurePolicy := admissionregistrationv1.Fail
	matchPolicy := admissionregistrationv1.Equivalent
	scope := admissionregistrationv1.NamespacedScope
	return contract.Scenario{
		APIVersion:           contract.ScenarioAPIVersion,
		Kind:                 contract.ScenarioKind,
		Metadata:             contract.ScenarioMetadata{Name: name},
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		Configuration: contract.WebhookConfiguration{Validating: &kube136.ValidatingWebhookConfiguration{
			TypeMeta: metav1.TypeMeta{APIVersion: kube136.AdmissionRegistrationAPIVersion, Kind: string(contract.ConfigurationKindValidating)},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{{
				Name:          "advanced.admitrace.io",
				FailurePolicy: &failurePolicy,
				MatchPolicy:   &matchPolicy,
				Rules: []admissionregistrationv1.RuleWithOperations{{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{ruleVersion},
						Resources:   []string{"deployments"},
						Scope:       &scope,
					},
				}},
			}},
		}},
		Request: contract.AdmissionRequest{
			Kind:      metav1.GroupVersionKind{Group: "apps", Version: requestVersion, Kind: "Deployment"},
			Resource:  metav1.GroupVersionResource{Group: "apps", Version: requestVersion, Resource: "deployments"},
			Name:      "demo",
			Namespace: "default",
			Operation: admissionv1.Create,
			Scope:     contract.RequestScopeNamespaced,
			Object:    []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"demo","namespace":"default"}}`),
			OldObject: nil,
			UserInfo:  authenticationUser(),
		},
	}
}

func exactFirstScenario() contract.Scenario {
	input := baseScenario("equivalent-exact-first", "v1", "v1")
	input.ExternalContext = &contract.ExternalContext{Equivalence: []contract.EquivalenceMapping{mapping("v1", "v1beta1")}}
	return input
}

func explicitMappingScenario() contract.Scenario {
	return equivalentScenario("equivalent-explicit-mapping", []contract.EquivalenceMapping{mapping("v1beta1", "v1")})
}

func equivalentScenario(name string, mappings []contract.EquivalenceMapping) contract.Scenario {
	input := baseScenario(name, "v1beta1", "v1")
	if mappings != nil {
		input.ExternalContext = &contract.ExternalContext{Equivalence: mappings}
	}
	return input
}

func selectorScenario(name string, ruleMatches bool) contract.Scenario {
	ruleVersion := "v2"
	if ruleMatches {
		ruleVersion = "v1"
	}
	input := baseScenario(name, "v1", ruleVersion)
	exact := admissionregistrationv1.Exact
	input.Configuration.Validating.Webhooks[0].MatchPolicy = &exact
	input.Configuration.Validating.Webhooks[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"environment": "prod"}}
	return input
}

func mapping(requestVersion, targetVersion string) contract.EquivalenceMapping {
	return contract.EquivalenceMapping{
		Request: contract.ResourceReference{GVR: metav1.GroupVersionResource{Group: "apps", Version: requestVersion, Resource: "deployments"}},
		Equivalents: []contract.EquivalentResource{{
			GVR: metav1.GroupVersionResource{Group: "apps", Version: targetVersion, Resource: "deployments"},
			GVK: metav1.GroupVersionKind{Group: "apps", Version: targetVersion, Kind: "Deployment"},
		}},
	}
}

func emptyMapping() []contract.EquivalenceMapping {
	return []contract.EquivalenceMapping{{
		Request:     contract.ResourceReference{GVR: metav1.GroupVersionResource{Group: "apps", Version: "v1beta1", Resource: "deployments"}},
		Equivalents: []contract.EquivalentResource{},
	}}
}

func authenticationUser() authenticationv1.UserInfo {
	return authenticationv1.UserInfo{Username: "fixture-user", Groups: []string{"fixture-group"}}
}

func trace(stage string, result contract.TraceResult, reason contract.ReasonCode, terminal bool) parity.TraceExpectation {
	return parity.TraceExpectation{Stage: stage, Result: result, ReasonCode: reason, Terminal: terminal}
}

func tracePending(stage string, result contract.TraceResult) parity.TraceExpectation {
	return parity.TraceExpectation{Stage: stage, Result: result, ReasonCode: contract.ReasonCodeEvaluationProblemPending, Pending: true}
}

func traceDiscarded(stage string, result contract.TraceResult) parity.TraceExpectation {
	return parity.TraceExpectation{Stage: stage, Result: result, ReasonCode: contract.ReasonCodeEvaluationProblemDiscarded, Discarded: true}
}
