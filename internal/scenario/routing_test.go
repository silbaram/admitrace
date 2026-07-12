package scenario_test

import (
	"encoding/json"
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/scenario"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateRejectsInvalidRouting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     contract.ConfigurationKind
		mutate   func(*contract.Scenario)
		wantPath string
	}{
		{
			name: "validating namespace selector",
			kind: contract.ConfigurationKindValidating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Validating.Webhooks[0].NamespaceSelector = invalidLabelSelector()
			},
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].namespaceSelector.matchExpressions[0].operator",
		},
		{
			name: "mutating object selector",
			kind: contract.ConfigurationKindMutating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Mutating.Webhooks[0].ObjectSelector = invalidLabelSelector()
			},
			wantPath: ".configuration.mutatingWebhookConfiguration.webhooks[0].objectSelector.matchExpressions[0].operator",
		},
		{
			name: "duplicate validating webhook name",
			kind: contract.ConfigurationKindValidating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Validating.Webhooks = append(
					input.Configuration.Validating.Webhooks,
					validValidatingWebhook("first.policy.example.com"),
				)
			},
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[1].name",
		},
		{
			name: "duplicate mutating webhook name",
			kind: contract.ConfigurationKindMutating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Mutating.Webhooks = append(
					input.Configuration.Mutating.Webhooks,
					validMutatingWebhook("first.policy.example.com"),
				)
			},
			wantPath: ".configuration.mutatingWebhookConfiguration.webhooks[1].name",
		},
		{
			name: "invalid validating webhook DNS name",
			kind: contract.ConfigurationKindValidating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Validating.Webhooks[0].Name = "Not_A_DNS_Name"
			},
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].name",
		},
		{
			name: "invalid mutating webhook DNS name",
			kind: contract.ConfigurationKindMutating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Mutating.Webhooks[0].Name = "policy.example"
			},
			wantPath: ".configuration.mutatingWebhookConfiguration.webhooks[0].name",
		},
		{
			name: "too many validating match conditions",
			kind: contract.ConfigurationKindValidating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Validating.Webhooks[0].MatchConditions = make([]admissionregistrationv1.MatchCondition, kube136.MaximumWebhookMatchConditions+1)
			},
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].matchConditions",
		},
		{
			name: "too many mutating match conditions",
			kind: contract.ConfigurationKindMutating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Mutating.Webhooks[0].MatchConditions = make([]admissionregistrationv1.MatchCondition, kube136.MaximumWebhookMatchConditions+1)
			},
			wantPath: ".configuration.mutatingWebhookConfiguration.webhooks[0].matchConditions",
		},
		{
			name: "invalid failure policy",
			kind: contract.ConfigurationKindValidating,
			mutate: func(input *contract.Scenario) {
				value := admissionregistrationv1.FailurePolicyType("Retry")
				input.Configuration.Validating.Webhooks[0].FailurePolicy = &value
			},
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].failurePolicy",
		},
		{
			name: "invalid match policy",
			kind: contract.ConfigurationKindMutating,
			mutate: func(input *contract.Scenario) {
				value := admissionregistrationv1.MatchPolicyType("Similar")
				input.Configuration.Mutating.Webhooks[0].MatchPolicy = &value
			},
			wantPath: ".configuration.mutatingWebhookConfiguration.webhooks[0].matchPolicy",
		},
		{
			name: "invalid validating rule scope",
			kind: contract.ConfigurationKindValidating,
			mutate: func(input *contract.Scenario) {
				value := admissionregistrationv1.ScopeType("Namespace")
				input.Configuration.Validating.Webhooks[0].Rules[0].Scope = &value
			},
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].rules[0].scope",
		},
		{
			name: "invalid mutating rule scope",
			kind: contract.ConfigurationKindMutating,
			mutate: func(input *contract.Scenario) {
				value := admissionregistrationv1.ScopeType("Namespace")
				input.Configuration.Mutating.Webhooks[0].Rules[0].Scope = &value
			},
			wantPath: ".configuration.mutatingWebhookConfiguration.webhooks[0].rules[0].scope",
		},
		{
			name: "invalid rule operation",
			kind: contract.ConfigurationKindValidating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Validating.Webhooks[0].Rules[0].Operations[0] = admissionregistrationv1.OperationType("PATCH")
			},
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].rules[0].operations[0]",
		},
		{
			name: "missing match condition name",
			kind: contract.ConfigurationKindMutating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Mutating.Webhooks[0].MatchConditions = []admissionregistrationv1.MatchCondition{{Expression: "true"}}
			},
			wantPath: ".configuration.mutatingWebhookConfiguration.webhooks[0].matchConditions[0].name",
		},
		{
			name: "invalid qualified match condition name",
			kind: contract.ConfigurationKindValidating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Validating.Webhooks[0].MatchConditions = []admissionregistrationv1.MatchCondition{{Name: "bad name", Expression: "true"}}
			},
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].matchConditions[0].name",
		},
		{
			name: "duplicate match condition name",
			kind: contract.ConfigurationKindValidating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Validating.Webhooks[0].MatchConditions = []admissionregistrationv1.MatchCondition{
					{Name: "check.example.com/ready", Expression: "true"},
					{Name: "check.example.com/ready", Expression: "false"},
				}
			},
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].matchConditions[1].name",
		},
		{
			name: "empty match condition expression",
			kind: contract.ConfigurationKindMutating,
			mutate: func(input *contract.Scenario) {
				input.Configuration.Mutating.Webhooks[0].MatchConditions = []admissionregistrationv1.MatchCondition{{Name: "check.example.com/ready", Expression: "  "}}
			},
			wantPath: ".configuration.mutatingWebhookConfiguration.webhooks[0].matchConditions[0].expression",
		},
		{
			name: "invalid request operation",
			kind: contract.ConfigurationKindValidating,
			mutate: func(input *contract.Scenario) {
				input.Request.Operation = admissionv1.Operation("PATCH")
			},
			wantPath: ".request.operation",
		},
		{
			name: "invalid request scope",
			kind: contract.ConfigurationKindMutating,
			mutate: func(input *contract.Scenario) {
				input.Request.Scope = contract.RequestScope("All")
			},
			wantPath: ".request.scope",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := newScenario(test.kind)
			test.mutate(input)
			err := scenario.Validate(input)
			requireInvalidInput(t, err, test.wantPath)
		})
	}
}

func TestDecodeAppliesRoutingDefaults(t *testing.T) {
	t.Parallel()

	for _, kind := range []contract.ConfigurationKind{
		contract.ConfigurationKindValidating,
		contract.ConfigurationKindMutating,
	} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(newScenario(kind))
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			got, err := scenario.Decode(data)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			assertRoutingValues(t, got, admissionregistrationv1.Fail, admissionregistrationv1.Equivalent, admissionregistrationv1.AllScopes)
			assertUnrelatedFieldsNotDefaulted(t, got)
		})
	}
}

func TestDecodePreservesExplicitRoutingValues(t *testing.T) {
	t.Parallel()

	for _, kind := range []contract.ConfigurationKind{
		contract.ConfigurationKindValidating,
		contract.ConfigurationKindMutating,
	} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			input := newScenario(kind)
			setRoutingValues(input, admissionregistrationv1.Ignore, admissionregistrationv1.Exact, admissionregistrationv1.ClusterScope)
			data, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			got, err := scenario.Decode(data)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			assertRoutingValues(t, got, admissionregistrationv1.Ignore, admissionregistrationv1.Exact, admissionregistrationv1.ClusterScope)
		})
	}
}

func TestValidateDoesNotApplyDefaultsOrMutateInvalidInput(t *testing.T) {
	t.Parallel()

	input := newScenario(contract.ConfigurationKindValidating)
	if err := scenario.Validate(input); err != nil {
		t.Fatalf("Validate() valid input error = %v", err)
	}
	webhook := &input.Configuration.Validating.Webhooks[0]
	if webhook.FailurePolicy != nil || webhook.MatchPolicy != nil || webhook.Rules[0].Scope != nil {
		t.Fatalf("Validate() applied defaults: failurePolicy=%v matchPolicy=%v scope=%v", webhook.FailurePolicy, webhook.MatchPolicy, webhook.Rules[0].Scope)
	}

	invalidPolicy := admissionregistrationv1.FailurePolicyType("Retry")
	webhook.FailurePolicy = &invalidPolicy
	err := scenario.Validate(input)
	requireInvalidInput(t, err, ".configuration.validatingWebhookConfiguration.webhooks[0].failurePolicy")
	if webhook.FailurePolicy == nil || *webhook.FailurePolicy != invalidPolicy {
		t.Errorf("FailurePolicy after failed validation = %v, want %q", webhook.FailurePolicy, invalidPolicy)
	}
	if webhook.MatchPolicy != nil || webhook.Rules[0].Scope != nil {
		t.Errorf("failed validation mutated defaults: matchPolicy=%v scope=%v", webhook.MatchPolicy, webhook.Rules[0].Scope)
	}
}

func invalidLabelSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "environment",
				Operator: metav1.LabelSelectorOperator("Between"),
				Values:   []string{"prod"},
			},
		},
	}
}

func setRoutingValues(
	input *contract.Scenario,
	failurePolicy admissionregistrationv1.FailurePolicyType,
	matchPolicy admissionregistrationv1.MatchPolicyType,
	scope admissionregistrationv1.ScopeType,
) {
	if input.Configuration.Validating != nil {
		webhook := &input.Configuration.Validating.Webhooks[0]
		webhook.FailurePolicy = &failurePolicy
		webhook.MatchPolicy = &matchPolicy
		webhook.Rules[0].Scope = &scope
		return
	}
	webhook := &input.Configuration.Mutating.Webhooks[0]
	webhook.FailurePolicy = &failurePolicy
	webhook.MatchPolicy = &matchPolicy
	webhook.Rules[0].Scope = &scope
}

func assertRoutingValues(
	t *testing.T,
	input *contract.Scenario,
	wantFailure admissionregistrationv1.FailurePolicyType,
	wantMatch admissionregistrationv1.MatchPolicyType,
	wantScope admissionregistrationv1.ScopeType,
) {
	t.Helper()

	var failurePolicy *admissionregistrationv1.FailurePolicyType
	var matchPolicy *admissionregistrationv1.MatchPolicyType
	var scope *admissionregistrationv1.ScopeType
	if input.Configuration.Validating != nil {
		webhook := &input.Configuration.Validating.Webhooks[0]
		failurePolicy = webhook.FailurePolicy
		matchPolicy = webhook.MatchPolicy
		scope = webhook.Rules[0].Scope
	} else {
		webhook := &input.Configuration.Mutating.Webhooks[0]
		failurePolicy = webhook.FailurePolicy
		matchPolicy = webhook.MatchPolicy
		scope = webhook.Rules[0].Scope
	}
	if failurePolicy == nil || *failurePolicy != wantFailure {
		t.Errorf("FailurePolicy = %v, want %q", failurePolicy, wantFailure)
	}
	if matchPolicy == nil || *matchPolicy != wantMatch {
		t.Errorf("MatchPolicy = %v, want %q", matchPolicy, wantMatch)
	}
	if scope == nil || *scope != wantScope {
		t.Errorf("Rule.Scope = %v, want %q", scope, wantScope)
	}
}

func assertUnrelatedFieldsNotDefaulted(t *testing.T, input *contract.Scenario) {
	t.Helper()

	if input.Configuration.Validating != nil {
		webhook := &input.Configuration.Validating.Webhooks[0]
		if webhook.SideEffects != nil || webhook.TimeoutSeconds != nil || webhook.AdmissionReviewVersions != nil {
			t.Errorf("unrelated validating fields were defaulted: sideEffects=%v timeoutSeconds=%v admissionReviewVersions=%v", webhook.SideEffects, webhook.TimeoutSeconds, webhook.AdmissionReviewVersions)
		}
		return
	}
	webhook := &input.Configuration.Mutating.Webhooks[0]
	if webhook.SideEffects != nil || webhook.TimeoutSeconds != nil || webhook.AdmissionReviewVersions != nil || webhook.ReinvocationPolicy != nil {
		t.Errorf("unrelated mutating fields were defaulted: sideEffects=%v timeoutSeconds=%v admissionReviewVersions=%v reinvocationPolicy=%v", webhook.SideEffects, webhook.TimeoutSeconds, webhook.AdmissionReviewVersions, webhook.ReinvocationPolicy)
	}
}
