package routing

import (
	"fmt"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/normalize"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

// NoMatchedRuleIndex identifies an Exact result with no matching rule.
const NoMatchedRuleIndex = -1

// ExactRuleEvaluation records one rule evaluation in input order.
type ExactRuleEvaluation struct {
	RuleIndex  int
	SourcePath string
	Matched    bool
	ReasonCode contract.ReasonCode
}

// ExactRulesResult contains the short-circuited Exact rules result and every
// rule evaluation that was actually performed. MatchedRuleIndex is
// NoMatchedRuleIndex when no rule matched.
type ExactRulesResult struct {
	Matched          bool
	MatchedRuleIndex int
	SourcePath       string
	ReasonCode       contract.ReasonCode
	Evaluations      []ExactRuleEvaluation
}

// MatchExactRules evaluates normalized rules in input order and stops at the
// first match, following Kubernetes 1.36 staging matcher semantics.
func MatchExactRules(webhook normalize.NormalizedWebhook, request normalize.RequestContext) ExactRulesResult {
	result := ExactRulesResult{
		MatchedRuleIndex: NoMatchedRuleIndex,
		SourcePath:       webhook.SourcePath + ".rules",
		ReasonCode:       contract.ReasonCodeRuleNoMatch,
		Evaluations:      make([]ExactRuleEvaluation, 0, len(webhook.Rules)),
	}
	for i, rule := range webhook.Rules {
		sourcePath := fmt.Sprintf("%s.rules[%d]", webhook.SourcePath, i)
		matched := kube136.ExactRuleMatches(
			compatibilityRule(rule),
			request.Operation,
			request.Resource,
			request.Subresource,
			request.Scope == contract.RequestScopeNamespaced,
		)
		reasonCode := contract.ReasonCodeRuleNoMatch
		if matched {
			reasonCode = contract.ReasonCodeRuleMatch
		}
		result.Evaluations = append(result.Evaluations, ExactRuleEvaluation{
			RuleIndex:  i,
			SourcePath: sourcePath,
			Matched:    matched,
			ReasonCode: reasonCode,
		})
		if matched {
			result.Matched = true
			result.MatchedRuleIndex = i
			result.SourcePath = sourcePath
			result.ReasonCode = contract.ReasonCodeRuleMatch
			return result
		}
	}
	return result
}

func compatibilityRule(rule normalize.Rule) admissionregistrationv1.RuleWithOperations {
	scope := rule.Scope
	return admissionregistrationv1.RuleWithOperations{
		Operations: rule.Operations,
		Rule: admissionregistrationv1.Rule{
			APIGroups:   rule.APIGroups,
			APIVersions: rule.APIVersions,
			Resources:   rule.Resources,
			Scope:       &scope,
		},
	}
}
