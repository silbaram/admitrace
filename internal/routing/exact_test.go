package routing_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/normalize"
	"github.com/silbaram/admitrace/internal/routing"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestMatchExactRulesPreservesOrderAndSourcePaths(t *testing.T) {
	t.Parallel()

	webhook := normalize.NormalizedWebhook{
		SourcePath: ".configuration.validatingWebhookConfiguration.webhooks[2]",
		Rules: []normalize.Rule{
			routingRule(admissionregistrationv1.Create, "apps", "v1", "deployments"),
			routingRule(admissionregistrationv1.Update, "apps", "v1", "deployments/status"),
			routingRule(admissionregistrationv1.OperationAll, "*", "*", "*/*"),
		},
	}
	request := normalize.RequestContext{
		Operation:   admissionv1.Update,
		Resource:    schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		Subresource: "status",
		Scope:       contract.RequestScopeNamespaced,
	}
	wantEvaluations := []routing.ExactRuleEvaluation{
		{
			RuleIndex:  0,
			SourcePath: ".configuration.validatingWebhookConfiguration.webhooks[2].rules[0]",
			ReasonCode: contract.ReasonCodeRuleNoMatch,
		},
		{
			RuleIndex:  1,
			SourcePath: ".configuration.validatingWebhookConfiguration.webhooks[2].rules[1]",
			Matched:    true,
			ReasonCode: contract.ReasonCodeRuleMatch,
		},
	}

	for i := 0; i < 25; i++ {
		got := routing.MatchExactRules(webhook, request)
		if !got.Matched {
			t.Fatalf("MatchExactRules() iteration %d Matched = false, want true", i)
		}
		if got.MatchedRuleIndex != 1 {
			t.Errorf("MatchExactRules() iteration %d MatchedRuleIndex = %d, want 1", i, got.MatchedRuleIndex)
		}
		if got.SourcePath != wantEvaluations[1].SourcePath {
			t.Errorf("MatchExactRules() iteration %d SourcePath = %q, want %q", i, got.SourcePath, wantEvaluations[1].SourcePath)
		}
		if got.ReasonCode != contract.ReasonCodeRuleMatch {
			t.Errorf("MatchExactRules() iteration %d ReasonCode = %q, want %q", i, got.ReasonCode, contract.ReasonCodeRuleMatch)
		}
		if !reflect.DeepEqual(got.Evaluations, wantEvaluations) {
			t.Errorf("MatchExactRules() iteration %d Evaluations = %#v, want %#v", i, got.Evaluations, wantEvaluations)
		}
	}
}

func TestMatchExactRulesReportsEveryNoMatch(t *testing.T) {
	t.Parallel()

	webhook := normalize.NormalizedWebhook{
		SourcePath: ".configuration.mutatingWebhookConfiguration.webhooks[0]",
		Rules: []normalize.Rule{
			routingRule(admissionregistrationv1.Create, "batch", "v1", "jobs"),
			routingRule(admissionregistrationv1.Delete, "apps", "v1", "deployments"),
		},
	}
	request := normalize.RequestContext{
		Operation: admissionv1.Update,
		Resource:  schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		Scope:     contract.RequestScopeCluster,
	}

	got := routing.MatchExactRules(webhook, request)
	if got.Matched {
		t.Error("MatchExactRules() Matched = true, want false")
	}
	if got.MatchedRuleIndex != routing.NoMatchedRuleIndex {
		t.Errorf("MatchExactRules() MatchedRuleIndex = %d, want %d", got.MatchedRuleIndex, routing.NoMatchedRuleIndex)
	}
	wantSourcePath := ".configuration.mutatingWebhookConfiguration.webhooks[0].rules"
	if got.SourcePath != wantSourcePath {
		t.Errorf("MatchExactRules() SourcePath = %q, want %q", got.SourcePath, wantSourcePath)
	}
	if got.ReasonCode != contract.ReasonCodeRuleNoMatch {
		t.Errorf("MatchExactRules() ReasonCode = %q, want %q", got.ReasonCode, contract.ReasonCodeRuleNoMatch)
	}
	if len(got.Evaluations) != 2 {
		t.Fatalf("len(MatchExactRules().Evaluations) = %d, want 2", len(got.Evaluations))
	}
	for i, evaluation := range got.Evaluations {
		if evaluation.RuleIndex != i {
			t.Errorf("Evaluations[%d].RuleIndex = %d, want %d", i, evaluation.RuleIndex, i)
		}
		if evaluation.Matched {
			t.Errorf("Evaluations[%d].Matched = true, want false", i)
		}
		if evaluation.ReasonCode != contract.ReasonCodeRuleNoMatch {
			t.Errorf("Evaluations[%d].ReasonCode = %q, want %q", i, evaluation.ReasonCode, contract.ReasonCodeRuleNoMatch)
		}
		wantPath := fmt.Sprintf(".configuration.mutatingWebhookConfiguration.webhooks[0].rules[%d]", i)
		if evaluation.SourcePath != wantPath {
			t.Errorf("Evaluations[%d].SourcePath = %q, want %q", i, evaluation.SourcePath, wantPath)
		}
	}
}

func TestMatchExactRulesReturnsExplicitEmptyEvaluationList(t *testing.T) {
	t.Parallel()

	got := routing.MatchExactRules(normalize.NormalizedWebhook{SourcePath: ".webhooks[0]"}, normalize.RequestContext{})
	if got.Matched {
		t.Error("MatchExactRules() Matched = true, want false")
	}
	if got.Evaluations == nil {
		t.Error("MatchExactRules() Evaluations = nil, want explicit empty list")
	}
	if len(got.Evaluations) != 0 {
		t.Errorf("len(MatchExactRules().Evaluations) = %d, want 0", len(got.Evaluations))
	}
	if got.SourcePath != ".webhooks[0].rules" {
		t.Errorf("MatchExactRules() SourcePath = %q, want %q", got.SourcePath, ".webhooks[0].rules")
	}
	if got.ReasonCode != contract.ReasonCodeRuleNoMatch {
		t.Errorf("MatchExactRules() ReasonCode = %q, want %q", got.ReasonCode, contract.ReasonCodeRuleNoMatch)
	}
}

func routingRule(
	operation admissionregistrationv1.OperationType,
	apiGroup string,
	apiVersion string,
	resource string,
) normalize.Rule {
	return normalize.Rule{
		Operations:  []admissionregistrationv1.OperationType{operation},
		APIGroups:   []string{apiGroup},
		APIVersions: []string{apiVersion},
		Resources:   []string{resource},
		Scope:       admissionregistrationv1.AllScopes,
	}
}
