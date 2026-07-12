package normalize_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/normalize"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWebhooksNormalizesValidatingAndMutatingSymmetrically(t *testing.T) {
	t.Parallel()

	validating, err := normalize.Webhooks(contract.WebhookConfiguration{
		Validating: newValidatingConfiguration("first.policy.example.com"),
	})
	if err != nil {
		t.Fatalf("Webhooks(validating) error = %v", err)
	}
	mutating, err := normalize.Webhooks(contract.WebhookConfiguration{
		Mutating: newMutatingConfiguration("first.policy.example.com"),
	})
	if err != nil {
		t.Fatalf("Webhooks(mutating) error = %v", err)
	}
	if len(validating) != 1 || len(mutating) != 1 {
		t.Fatalf("normalized lengths = (%d, %d), want (1, 1)", len(validating), len(mutating))
	}

	if validating[0].ConfigurationKind != contract.ConfigurationKindValidating {
		t.Errorf("validating ConfigurationKind = %q, want %q", validating[0].ConfigurationKind, contract.ConfigurationKindValidating)
	}
	if validating[0].SourcePath != ".configuration.validatingWebhookConfiguration.webhooks[0]" {
		t.Errorf("validating SourcePath = %q, want exact validating path", validating[0].SourcePath)
	}
	if mutating[0].ConfigurationKind != contract.ConfigurationKindMutating {
		t.Errorf("mutating ConfigurationKind = %q, want %q", mutating[0].ConfigurationKind, contract.ConfigurationKindMutating)
	}
	if mutating[0].SourcePath != ".configuration.mutatingWebhookConfiguration.webhooks[0]" {
		t.Errorf("mutating SourcePath = %q, want exact mutating path", mutating[0].SourcePath)
	}

	validatingCommon := validating[0]
	validatingCommon.ConfigurationKind = mutating[0].ConfigurationKind
	validatingCommon.SourcePath = mutating[0].SourcePath
	if !reflect.DeepEqual(validatingCommon, mutating[0]) {
		t.Errorf("common validating webhook = %#v, want mutating common fields %#v", validatingCommon, mutating[0])
	}
}

func TestWebhooksPreservesOrderIndexPathsAndNestedOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      contract.WebhookConfiguration
		kind       contract.ConfigurationKind
		pathPrefix string
	}{
		{
			name:       "validating",
			input:      contract.WebhookConfiguration{Validating: newValidatingConfiguration("first.policy.example.com", "second.policy.example.com")},
			kind:       contract.ConfigurationKindValidating,
			pathPrefix: ".configuration.validatingWebhookConfiguration.webhooks",
		},
		{
			name:       "mutating",
			input:      contract.WebhookConfiguration{Mutating: newMutatingConfiguration("first.policy.example.com", "second.policy.example.com")},
			kind:       contract.ConfigurationKindMutating,
			pathPrefix: ".configuration.mutatingWebhookConfiguration.webhooks",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalize.Webhooks(test.input)
			if err != nil {
				t.Fatalf("Webhooks() error = %v", err)
			}
			if len(got) != 2 {
				t.Fatalf("len(Webhooks()) = %d, want 2", len(got))
			}
			for i, wantName := range []string{"first.policy.example.com", "second.policy.example.com"} {
				if got[i].Name != wantName || got[i].InputIndex != i || got[i].ConfigurationKind != test.kind {
					t.Errorf("webhook[%d] identity = (%q, %d, %q), want (%q, %d, %q)", i, got[i].Name, got[i].InputIndex, got[i].ConfigurationKind, wantName, i, test.kind)
				}
				wantPath := fmt.Sprintf("%s[%d]", test.pathPrefix, i)
				if got[i].SourcePath != wantPath {
					t.Errorf("webhook[%d].SourcePath = %q, want %q", i, got[i].SourcePath, wantPath)
				}
				if got[i].Rules[0].Resources[0] != "pods" || got[i].Rules[1].Resources[0] != "pods/status" {
					t.Errorf("webhook[%d] rule resource order = (%q, %q), want (pods, pods/status)", i, got[i].Rules[0].Resources[0], got[i].Rules[1].Resources[0])
				}
				if got[i].MatchConditions[0].Name != "first-check" || got[i].MatchConditions[1].Name != "second-check" {
					t.Errorf("webhook[%d] condition order = (%q, %q), want (first-check, second-check)", i, got[i].MatchConditions[0].Name, got[i].MatchConditions[1].Name)
				}
			}
		})
	}
}

func TestWebhooksPreservesNilAndExplicitEmptyLists(t *testing.T) {
	t.Parallel()

	nilConfiguration := &kube136.ValidatingWebhookConfiguration{}
	nilResult, err := normalize.Webhooks(contract.WebhookConfiguration{Validating: nilConfiguration})
	if err != nil {
		t.Fatalf("Webhooks(nil list) error = %v", err)
	}
	if nilResult != nil {
		t.Errorf("Webhooks(nil list) = %#v, want nil", nilResult)
	}

	emptyConfiguration := &kube136.MutatingWebhookConfiguration{Webhooks: []admissionregistrationv1.MutatingWebhook{}}
	emptyResult, err := normalize.Webhooks(contract.WebhookConfiguration{Mutating: emptyConfiguration})
	if err != nil {
		t.Fatalf("Webhooks(empty list) error = %v", err)
	}
	if emptyResult == nil || len(emptyResult) != 0 {
		t.Errorf("Webhooks(empty list) = %#v, want non-nil empty", emptyResult)
	}
}

func TestWebhooksDeepCopiesMutableRoutingFields(t *testing.T) {
	t.Parallel()

	t.Run("input mutation", func(t *testing.T) {
		configuration := newValidatingConfiguration("first.policy.example.com")
		pristine := configuration.DeepCopy()
		got, err := normalize.Webhooks(contract.WebhookConfiguration{Validating: configuration})
		if err != nil {
			t.Fatalf("Webhooks() error = %v", err)
		}
		want, err := normalize.Webhooks(contract.WebhookConfiguration{Validating: pristine})
		if err != nil {
			t.Fatalf("Webhooks(pristine) error = %v", err)
		}

		webhook := &configuration.Webhooks[0]
		webhook.Name = "changed.policy.example.com"
		webhook.Rules[0].Operations[0] = admissionregistrationv1.Delete
		webhook.Rules[0].APIGroups[0] = "changed.example.com"
		webhook.Rules[0].APIVersions[0] = "v9"
		webhook.Rules[0].Resources[0] = "changed"
		*webhook.Rules[0].Scope = admissionregistrationv1.ClusterScope
		*webhook.MatchPolicy = admissionregistrationv1.Equivalent
		*webhook.FailurePolicy = admissionregistrationv1.Fail
		webhook.NamespaceSelector.MatchLabels["environment"] = "changed"
		webhook.NamespaceSelector.MatchExpressions[0].Values[0] = "changed"
		webhook.ObjectSelector.MatchLabels["selected"] = "changed"
		webhook.MatchConditions[0].Expression = "false"
		*webhook.SideEffects = admissionregistrationv1.SideEffectClassSome
		webhook.AdmissionReviewVersions[0] = "v2"

		if !reflect.DeepEqual(got, want) {
			t.Errorf("normalized webhooks changed with input: got %#v, want %#v", got, want)
		}
	})

	t.Run("normalized mutation", func(t *testing.T) {
		configuration := newMutatingConfiguration("first.policy.example.com")
		want := configuration.DeepCopy()
		got, err := normalize.Webhooks(contract.WebhookConfiguration{Mutating: configuration})
		if err != nil {
			t.Fatalf("Webhooks() error = %v", err)
		}

		got[0].Rules[0].Operations[0] = admissionregistrationv1.Delete
		got[0].Rules[0].APIGroups[0] = "changed.example.com"
		got[0].Rules[0].APIVersions[0] = "v9"
		got[0].Rules[0].Resources[0] = "changed"
		got[0].Rules[0].Scope = admissionregistrationv1.ClusterScope
		got[0].MatchPolicy = admissionregistrationv1.Equivalent
		got[0].FailurePolicy = admissionregistrationv1.Fail
		got[0].NamespaceSelector.MatchLabels["environment"] = "changed"
		got[0].NamespaceSelector.MatchExpressions[0].Values[0] = "changed"
		got[0].ObjectSelector.MatchLabels["selected"] = "changed"
		got[0].MatchConditions[0].Expression = "false"
		*got[0].SideEffects = admissionregistrationv1.SideEffectClassSome
		got[0].AdmissionReviewVersions[0] = "v2"

		if !reflect.DeepEqual(configuration, want) {
			t.Errorf("input changed with normalized result: got %#v, want %#v", configuration, want)
		}
	})
}

func TestWebhooksRejectsImpossibleOneOfAndMissingDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    func() contract.WebhookConfiguration
		wantPath string
	}{
		{
			name:     "no configuration",
			input:    func() contract.WebhookConfiguration { return contract.WebhookConfiguration{} },
			wantPath: ".configuration",
		},
		{
			name: "both configurations",
			input: func() contract.WebhookConfiguration {
				return contract.WebhookConfiguration{Validating: &kube136.ValidatingWebhookConfiguration{}, Mutating: &kube136.MutatingWebhookConfiguration{}}
			},
			wantPath: ".configuration",
		},
		{
			name: "missing validating match policy",
			input: func() contract.WebhookConfiguration {
				configuration := newValidatingConfiguration("first.policy.example.com")
				configuration.Webhooks[0].MatchPolicy = nil
				return contract.WebhookConfiguration{Validating: configuration}
			},
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].matchPolicy",
		},
		{
			name: "missing mutating failure policy",
			input: func() contract.WebhookConfiguration {
				configuration := newMutatingConfiguration("first.policy.example.com")
				configuration.Webhooks[0].FailurePolicy = nil
				return contract.WebhookConfiguration{Mutating: configuration}
			},
			wantPath: ".configuration.mutatingWebhookConfiguration.webhooks[0].failurePolicy",
		},
		{
			name: "missing validating rule scope",
			input: func() contract.WebhookConfiguration {
				configuration := newValidatingConfiguration("first.policy.example.com")
				configuration.Webhooks[0].Rules[1].Scope = nil
				return contract.WebhookConfiguration{Validating: configuration}
			},
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].rules[1].scope",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalize.Webhooks(test.input())
			requireInvalidInput(t, err, test.wantPath)
		})
	}
}

type routingFields struct {
	rules                   []admissionregistrationv1.RuleWithOperations
	matchPolicy             *admissionregistrationv1.MatchPolicyType
	namespaceSelector       *metav1.LabelSelector
	objectSelector          *metav1.LabelSelector
	matchConditions         []admissionregistrationv1.MatchCondition
	failurePolicy           *admissionregistrationv1.FailurePolicyType
	sideEffects             *admissionregistrationv1.SideEffectClass
	admissionReviewVersions []string
}

func newRoutingFields() routingFields {
	matchPolicy := admissionregistrationv1.Exact
	failurePolicy := admissionregistrationv1.Ignore
	sideEffects := admissionregistrationv1.SideEffectClassNone
	namespacedScope := admissionregistrationv1.NamespacedScope
	allScopes := admissionregistrationv1.AllScopes
	return routingFields{
		rules: []admissionregistrationv1.RuleWithOperations{
			{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
					Scope:       &namespacedScope,
				},
			},
			{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods/status"},
					Scope:       &allScopes,
				},
			},
		},
		matchPolicy: &matchPolicy,
		namespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"environment": "production"},
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "tier", Operator: metav1.LabelSelectorOpIn, Values: []string{"frontend", "backend"}},
			},
		},
		objectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"selected": "true"}},
		matchConditions: []admissionregistrationv1.MatchCondition{
			{Name: "first-check", Expression: "request.operation == 'CREATE'"},
			{Name: "second-check", Expression: "object != null"},
		},
		failurePolicy:           &failurePolicy,
		sideEffects:             &sideEffects,
		admissionReviewVersions: []string{"v1", "v1beta1"},
	}
}

func newValidatingConfiguration(names ...string) *kube136.ValidatingWebhookConfiguration {
	configuration := &kube136.ValidatingWebhookConfiguration{}
	if names == nil {
		return configuration
	}
	configuration.Webhooks = make([]admissionregistrationv1.ValidatingWebhook, len(names))
	for i, name := range names {
		fields := newRoutingFields()
		configuration.Webhooks[i] = admissionregistrationv1.ValidatingWebhook{
			Name:                    name,
			Rules:                   fields.rules,
			MatchPolicy:             fields.matchPolicy,
			NamespaceSelector:       fields.namespaceSelector,
			ObjectSelector:          fields.objectSelector,
			MatchConditions:         fields.matchConditions,
			FailurePolicy:           fields.failurePolicy,
			SideEffects:             fields.sideEffects,
			AdmissionReviewVersions: fields.admissionReviewVersions,
		}
	}
	return configuration
}

func newMutatingConfiguration(names ...string) *kube136.MutatingWebhookConfiguration {
	configuration := &kube136.MutatingWebhookConfiguration{}
	if names == nil {
		return configuration
	}
	configuration.Webhooks = make([]admissionregistrationv1.MutatingWebhook, len(names))
	for i, name := range names {
		fields := newRoutingFields()
		configuration.Webhooks[i] = admissionregistrationv1.MutatingWebhook{
			Name:                    name,
			Rules:                   fields.rules,
			MatchPolicy:             fields.matchPolicy,
			NamespaceSelector:       fields.namespaceSelector,
			ObjectSelector:          fields.objectSelector,
			MatchConditions:         fields.matchConditions,
			FailurePolicy:           fields.failurePolicy,
			SideEffects:             fields.sideEffects,
			AdmissionReviewVersions: fields.admissionReviewVersions,
		}
	}
	return configuration
}

func requireInvalidInput(t *testing.T, err error, wantPath string) {
	t.Helper()

	if err == nil {
		t.Fatal("error = nil, want invalid input")
	}
	if !errors.Is(err, contract.ErrInvalidInput) {
		t.Errorf("errors.Is(error, ErrInvalidInput) = false; error = %v", err)
	}
	var invalid *contract.InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("errors.As(error, *InvalidInputError) = false; error = %v", err)
	}
	if invalid.Field != wantPath {
		t.Errorf("InvalidInputError.Field = %q, want %q", invalid.Field, wantPath)
	}
}
