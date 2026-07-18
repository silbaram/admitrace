package manifest

import (
	"context"
	"regexp"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

var identityReferencePattern = regexp.MustCompile(`\brequest\s*\.\s*userInfo\b`)

// AdapterEvaluation contains the unchanged evaluator result plus concerns that
// belong to manifest adaptation rather than one Scenario's result schema.
type AdapterEvaluation struct {
	Result      contract.EvaluationResult
	Diagnostics []Diagnostic
}

// EvaluateBuiltScenario evaluates one builder output and applies the explicit
// identity guard before returning adapter diagnostics. Missing authorizer
// fixtures retain the evaluator's existing fail-closed behavior.
func EvaluateBuiltScenario(ctx context.Context, built BuiltScenario) (AdapterEvaluation, error) {
	snapshot, err := evaluation.SnapshotFromScenario(built.Scenario)
	if err != nil {
		return AdapterEvaluation{}, &contract.InternalError{Operation: "prepare manifest Scenario", Err: err}
	}
	result := evaluation.NewEvaluator().Evaluate(ctx, snapshot)
	adapterDiagnostics := make([]Diagnostic, 0, 2)
	identityMissing := false
	for index := range result.Webhooks {
		webhook := &result.Webhooks[index]
		if built.IdentityProvided || !webhookRequiresIdentity(built.Scenario.Configuration, webhook.WebhookIndex) {
			continue
		}
		if !markMissingIdentity(webhook) {
			continue
		}
		identityMissing = true
	}
	if identityMissing {
		adapterDiagnostics = append(adapterDiagnostics, Diagnostic{
			Code:          DiagnosticCodeIdentityContextMissing,
			Severity:      contract.DiagnosticSeverityWarning,
			Message:       "matchConditions require explicit admission identity; provide --user and optional identity flags",
			SourceLabel:   built.Resource.Label,
			DocumentIndex: built.Resource.DocumentIndex,
		})
	}
	if hasActiveAuthorizationGap(result) {
		adapterDiagnostics = append(adapterDiagnostics, Diagnostic{
			Code:          DiagnosticCodeIncomplete,
			Severity:      contract.DiagnosticSeverityWarning,
			Message:       "matchConditions require authorization context that GET/LIST hydration cannot infer",
			SourceLabel:   built.Resource.Label,
			DocumentIndex: built.Resource.DocumentIndex,
		})
	}
	if err := result.Validate(); err != nil {
		return AdapterEvaluation{}, &contract.InternalError{Operation: "validate guarded manifest result", Err: err}
	}
	return AdapterEvaluation{Result: result, Diagnostics: adapterDiagnostics}, nil
}

func webhookRequiresIdentity(configuration contract.WebhookConfiguration, index int) bool {
	conditions := matchConditionsAt(configuration, index)
	for _, condition := range conditions {
		if identityReferencePattern.MatchString(condition.Expression) {
			return true
		}
	}
	return false
}

func matchConditionsAt(configuration contract.WebhookConfiguration, index int) []admissionregistrationv1.MatchCondition {
	if configuration.Validating != nil && index >= 0 && index < len(configuration.Validating.Webhooks) {
		return configuration.Validating.Webhooks[index].MatchConditions
	}
	if configuration.Mutating != nil && index >= 0 && index < len(configuration.Mutating.Webhooks) {
		return configuration.Mutating.Webhooks[index].MatchConditions
	}
	return nil
}

func markMissingIdentity(webhook *contract.WebhookEvaluation) bool {
	firstMatchCondition := -1
	for index, step := range webhook.Trace {
		if step.Stage == "matchConditions" {
			firstMatchCondition = index
			break
		}
	}
	if firstMatchCondition < 0 {
		return false
	}
	webhook.Trace = webhook.Trace[:firstMatchCondition]
	for index := range webhook.Trace {
		webhook.Trace[index].Terminal = false
	}
	webhook.Determination = contract.DeterminationIndeterminate
	webhook.Outcome = nil
	webhook.Diagnostics = []contract.Diagnostic{
		{
			Code:       contract.ReasonCodeIdentityContextMissing,
			Severity:   contract.DiagnosticSeverityWarning,
			Message:    "explicit admission identity is required to evaluate matchConditions",
			SourcePath: webhook.SourcePath + ".matchConditions",
			MissingContext: &contract.MissingContextDetail{
				Context: "admission identity",
			},
		},
	}
	webhook.Trace = append(webhook.Trace, contract.TraceStep{
		Stage:        "matchConditions",
		Sequence:     len(webhook.Trace),
		SourcePath:   webhook.SourcePath + ".matchConditions",
		InputSummary: contract.InputSummary{"identity": "not-explicitly-provided"},
		Result:       contract.TraceResultIndeterminate,
		ReasonCode:   contract.ReasonCodeIdentityContextMissing,
		Terminal:     true,
	})
	return true
}

func hasActiveAuthorizationGap(result contract.EvaluationResult) bool {
	for _, webhook := range result.Webhooks {
		if webhook.Determination != contract.DeterminationIndeterminate {
			continue
		}
		for _, step := range webhook.Trace {
			if step.Terminal && step.ReasonCode == contract.ReasonCodeAuthorizationContextMissing {
				return true
			}
		}
	}
	return false
}
