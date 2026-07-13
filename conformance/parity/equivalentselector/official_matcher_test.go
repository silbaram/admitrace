package equivalentselector

import (
	"errors"
	"testing"

	"github.com/silbaram/admitrace/conformance/parity"
	"github.com/silbaram/admitrace/internal/contract"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	officialnamespace "k8s.io/apiserver/pkg/admission/plugin/webhook/predicates/namespace"
	officialrules "k8s.io/apiserver/pkg/admission/plugin/webhook/predicates/rules"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestOfficialMatcherDifferential(t *testing.T) {
	tests := []struct {
		name        string
		ruleVersion string
	}{
		{name: "selector-pending-error-discarded", ruleVersion: "v2"},
		{name: "selector-applicable-error-rejected", ruleVersion: "v1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testCase := findDifferentialCase(t, test.name)
			if testCase.OracleType != parity.OracleOfficialMatcherDifferential {
				t.Fatalf("Scenario oracleType = %q, want %q", testCase.OracleType, parity.OracleOfficialMatcherDifferential)
			}
			product, err := evaluate(t, testCase)
			if err != nil {
				t.Fatal(err)
			}
			if err := parity.CompareEvaluation(testCase, product); err != nil {
				t.Fatal(err)
			}
			oracleOutcome, err := officialSelectorOutcome(test.ruleVersion)
			if err != nil {
				t.Fatal(err)
			}
			if product.Outcome == nil {
				t.Fatalf("official matcher differential outcome: got <nil>, want %q", oracleOutcome)
			}
			if got := *product.Outcome; got != oracleOutcome {
				t.Fatalf("official matcher differential outcome: got %q, want %q", got, oracleOutcome)
			}
		})
	}
}

func findDifferentialCase(t *testing.T, name string) parity.Case {
	t.Helper()
	for _, testCase := range cases() {
		if testCase.Scenario.Metadata.Name == name {
			return testCase
		}
	}
	t.Fatalf("Scenario %q not found", name)
	return parity.Case{}
}

func officialSelectorOutcome(ruleVersion string) (contract.Outcome, error) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("get", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected namespace lookup error")
	})
	selectors := informers.NewSharedInformerFactory(client, 0).Core().V1().Namespaces().Lister()
	namespaceMatcher := officialnamespace.Matcher{NamespaceLister: selectors, Client: client}

	scope := admissionregistrationv1.NamespacedScope
	hook := officialSelectorProvider{selector: labels.SelectorFromSet(map[string]string{"environment": "prod"})}
	rules := []admissionregistrationv1.RuleWithOperations{{
		Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
		Rule: admissionregistrationv1.Rule{
			APIGroups: []string{"apps"}, APIVersions: []string{ruleVersion}, Resources: []string{"deployments"}, Scope: &scope,
		},
	}}
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "demo", "namespace": "default"},
	}}
	attributes := admission.NewAttributesRecord(
		object, nil, schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		"default", "demo", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		"", admission.Create, nil, false, nil,
	)
	matchesNamespace, statusErr := namespaceMatcher.MatchNamespaceSelector(hook, attributes)
	if !matchesNamespace && statusErr == nil {
		return contract.OutcomeSkipped, nil
	}
	matchesRule := false
	for _, rule := range rules {
		if (&officialrules.Matcher{Rule: rule, Attr: attributes}).Matches() {
			matchesRule = true
			break
		}
	}
	switch {
	case !matchesRule:
		return contract.OutcomeSkipped, nil
	case statusErr != nil:
		return contract.OutcomeRejectedBeforeCall, nil
	default:
		return contract.OutcomeCalled, nil
	}
}

type officialSelectorProvider struct {
	selector labels.Selector
}

func (p officialSelectorProvider) GetParsedNamespaceSelector() (labels.Selector, error) {
	return p.selector, nil
}
