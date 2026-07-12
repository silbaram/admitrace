package evaluation

import (
	"context"
	"errors"
	"fmt"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	"github.com/silbaram/admitrace/internal/matchcondition"
	"github.com/silbaram/admitrace/internal/normalize"
	"github.com/silbaram/admitrace/internal/routing"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

// Snapshot contains the immutable inputs used to evaluate every configured
// webhook independently.
type Snapshot struct {
	ScenarioID           string
	CompatibilityProfile contract.CompatibilityProfile
	ConfigurationKind    contract.ConfigurationKind
	Webhooks             []normalize.NormalizedWebhook
	Request              normalize.RequestContext
	Namespaces           fixture.NamespaceProvider
	Equivalents          routing.EquivalentResourceLookup
	Authorizer           fixture.Authorizer
}

// SnapshotFromScenario prepares owned routing and fixture inputs from a
// defaulted, validated Scenario.
func SnapshotFromScenario(input contract.Scenario) (Snapshot, error) {
	webhooks, err := normalize.Webhooks(input.Configuration)
	if err != nil {
		return Snapshot{}, fmt.Errorf("normalize webhooks: %w", err)
	}
	request, err := normalize.BuildRequestContext(input.Request)
	if err != nil {
		return Snapshot{}, fmt.Errorf("build request context: %w", err)
	}
	kind, ok := input.Configuration.Kind()
	if !ok {
		return Snapshot{}, &contract.InvalidInputError{Field: ".configuration", Err: errors.New("must contain exactly one webhook configuration")}
	}

	var namespaceEntries []fixture.NamespaceEntry
	var authorization []contract.AuthorizationDecision
	var equivalence []contract.EquivalenceMapping
	if input.ExternalContext != nil {
		authorization = input.ExternalContext.Authorization
		equivalence = input.ExternalContext.Equivalence
		if input.ExternalContext.Namespace != nil {
			namespaceEntries = []fixture.NamespaceEntry{
				{Name: input.ExternalContext.Namespace.Name, Namespace: input.ExternalContext.Namespace},
			}
		}
	}
	namespaces, err := fixture.NewNamespaceProvider(namespaceEntries...)
	if err != nil {
		return Snapshot{}, fmt.Errorf("build namespace fixture: %w", err)
	}
	equivalents, err := fixture.NewEquivalentResourceMapper(equivalence)
	if err != nil {
		return Snapshot{}, fmt.Errorf("build equivalence fixture: %w", err)
	}
	authorizer, err := fixture.NewAuthorizer(authorization)
	if err != nil {
		return Snapshot{}, fmt.Errorf("build authorization fixture: %w", err)
	}
	return Snapshot{
		ScenarioID:           input.Metadata.Name,
		CompatibilityProfile: input.CompatibilityProfile,
		ConfigurationKind:    kind,
		Webhooks:             webhooks,
		Request:              request,
		Namespaces:           namespaces,
		Equivalents:          equivalents,
		Authorizer:           authorizer,
	}, nil
}

// Evaluator combines the Kubernetes 1.36 pre-CEL and matchConditions stages.
type Evaluator struct {
	matchConditions matchcondition.Evaluator
}

// NewEvaluator creates a reusable offline snapshot evaluator.
func NewEvaluator() Evaluator {
	return Evaluator{matchConditions: matchcondition.NewEvaluator()}
}

// Evaluate evaluates every webhook in input order against the same snapshot.
// A mutating result describes initial eligibility only; no patch, reinvocation,
// AdmissionReview negotiation, or transport is performed.
func (e Evaluator) Evaluate(ctx context.Context, snapshot Snapshot) contract.EvaluationResult {
	result := contract.EvaluationResult{
		SchemaVersion:        contract.ResultSchemaVersion,
		ScenarioID:           snapshot.ScenarioID,
		CompatibilityProfile: snapshot.CompatibilityProfile,
		EvaluationPhase:      contract.EvaluationPhaseSnapshotRouting,
		ConfigurationKind:    snapshot.ConfigurationKind,
		Webhooks:             make([]contract.WebhookEvaluation, len(snapshot.Webhooks)),
	}
	if snapshot.ConfigurationKind == contract.ConfigurationKindMutating {
		result.EvaluationPhase = contract.EvaluationPhaseMutatingInitialSnapshot
		result.Diagnostics = append(result.Diagnostics, mutatingScopeDiagnostic())
	}

	for i, webhook := range snapshot.Webhooks {
		result.Webhooks[i] = e.evaluateWebhook(ctx, snapshot, webhook)
	}
	return result
}

func (e Evaluator) evaluateWebhook(
	ctx context.Context,
	snapshot Snapshot,
	webhook normalize.NormalizedWebhook,
) contract.WebhookEvaluation {
	evaluation := contract.WebhookEvaluation{
		ConfigurationKind: webhook.ConfigurationKind,
		WebhookName:       webhook.Name,
		WebhookIndex:      webhook.InputIndex,
		SourcePath:        webhook.SourcePath,
	}
	preCEL := routing.ShouldCallHookPreCEL(webhook, snapshot.Request, snapshot.Namespaces, snapshot.Equivalents)
	evaluation.Trace = cloneTrace(preCEL.Trace)

	switch preCEL.Status {
	case routing.PreCELStatusSkipped:
		evaluation.Determination = contract.DeterminationDeterminate
		evaluation.Outcome = preCEL.Outcome
		return evaluation
	case routing.PreCELStatusProblem:
		return completeProblem(evaluation, preCEL.Err)
	case routing.PreCELStatusContinue:
		if preCEL.Continuation == nil {
			return completeProblem(evaluation, &contract.InternalError{Operation: "continue pre-CEL routing without invocation"})
		}
	default:
		return completeProblem(evaluation, &contract.InternalError{Operation: "classify pre-CEL status"})
	}

	invocation := preCEL.Continuation.Invocation
	matched := e.matchConditions.Evaluate(
		ctx,
		webhook,
		snapshot.Request,
		matchcondition.Invocation{
			Resource:    invocation.Resource,
			Subresource: invocation.Subresource,
			Kind:        invocation.Kind,
		},
		snapshot.Authorizer,
	)
	evaluation.Determination = matched.Determination
	evaluation.Outcome = matched.Outcome
	evaluation.Trace = append(evaluation.Trace, matched.Trace...)
	resequence(evaluation.Trace)
	if matched.Err != nil {
		evaluation.Diagnostics = append(evaluation.Diagnostics, diagnosticFor(matched.Err, matched.ReasonCode, webhook.SourcePath+".matchConditions"))
	}

	if evaluation.Outcome != nil && *evaluation.Outcome == contract.OutcomeCalled {
		if dryRunUnsupported(snapshot.Request, webhook) {
			return markDryRunUnsupported(evaluation, webhook)
		}
		evaluation.Diagnostics = append(evaluation.Diagnostics, transportScopeDiagnostic(webhook))
	}
	return evaluation
}

func completeProblem(evaluation contract.WebhookEvaluation, err error) contract.WebhookEvaluation {
	reason := terminalReason(evaluation.Trace)
	switch {
	case errors.Is(err, contract.ErrKubernetesEvaluation):
		evaluation.Determination = contract.DeterminationDeterminate
		evaluation.Outcome = outcome(contract.OutcomeRejectedBeforeCall)
	case errors.Is(err, contract.ErrUnsupportedCapability):
		evaluation.Determination = contract.DeterminationUnsupported
	case errors.Is(err, contract.ErrMissingContext):
		evaluation.Determination = contract.DeterminationIndeterminate
	default:
		evaluation.Determination = contract.DeterminationIndeterminate
		ensureTerminalIndeterminate(&evaluation, reason)
	}
	evaluation.Diagnostics = append(evaluation.Diagnostics, diagnosticFor(err, reason, terminalSource(evaluation)))
	return evaluation
}

func dryRunUnsupported(request normalize.RequestContext, webhook normalize.NormalizedWebhook) bool {
	if !request.DryRun || webhook.SideEffects == nil {
		return false
	}
	return *webhook.SideEffects != admissionregistrationv1.SideEffectClassNone &&
		*webhook.SideEffects != admissionregistrationv1.SideEffectClassNoneOnDryRun
}

func markDryRunUnsupported(
	evaluation contract.WebhookEvaluation,
	webhook normalize.NormalizedWebhook,
) contract.WebhookEvaluation {
	evaluation.Determination = contract.DeterminationUnsupported
	evaluation.Outcome = nil
	if len(evaluation.Trace) > 0 {
		evaluation.Trace[len(evaluation.Trace)-1].Terminal = false
	}
	evaluation.Trace = append(evaluation.Trace, contract.TraceStep{
		Stage:        "dryRunSideEffects",
		SourcePath:   webhook.SourcePath + ".sideEffects",
		InputSummary: contract.InputSummary{"dryRun": "true", "sideEffects": string(*webhook.SideEffects)},
		Result:       contract.TraceResultUnsupported,
		ReasonCode:   contract.ReasonCodeCapabilityOutsideProfile,
		Terminal:     true,
	})
	resequence(evaluation.Trace)
	evaluation.Diagnostics = append(evaluation.Diagnostics, contract.Diagnostic{
		Code:       contract.ReasonCodeCapabilityOutsideProfile,
		Severity:   contract.DiagnosticSeverityWarning,
		Message:    "dry-run compatibility is not available for this webhook sideEffects value",
		SourcePath: webhook.SourcePath + ".sideEffects",
		UnsupportedCapability: &contract.UnsupportedCapabilityDetail{
			Capability: "dry-run webhook invocation with side effects",
			Detail:     "snapshot evaluation does not claim that the webhook can be called",
		},
	})
	return evaluation
}

func mutatingScopeDiagnostic() contract.Diagnostic {
	return contract.Diagnostic{
		Code:       contract.ReasonCodeCapabilityOutsideProfile,
		Severity:   contract.DiagnosticSeverityInfo,
		Message:    "mutating results describe initial snapshot eligibility only",
		SourcePath: ".configuration.mutatingWebhookConfiguration",
		UnsupportedCapability: &contract.UnsupportedCapabilityDetail{
			Capability: "mutating patch chain and reinvocation",
			Detail:     "patch application and reinvocation are not evaluated",
		},
	}
}

func transportScopeDiagnostic(webhook normalize.NormalizedWebhook) contract.Diagnostic {
	return contract.Diagnostic{
		Code:       contract.ReasonCodeCapabilityOutsideProfile,
		Severity:   contract.DiagnosticSeverityInfo,
		Message:    "called means snapshot eligibility; AdmissionReview negotiation and transport were not evaluated",
		SourcePath: webhook.SourcePath + ".admissionReviewVersions",
		UnsupportedCapability: &contract.UnsupportedCapabilityDetail{
			Capability: "AdmissionReview negotiation and webhook transport",
			Detail:     "no network request was made",
		},
	}
}

func diagnosticFor(err error, reason contract.ReasonCode, sourcePath string) contract.Diagnostic {
	diagnostic := contract.Diagnostic{
		Code:       reason,
		Severity:   contract.DiagnosticSeverityError,
		Message:    "webhook snapshot evaluation did not complete",
		SourcePath: sourcePath,
	}
	var missing *contract.MissingContextError
	if errors.As(err, &missing) {
		diagnostic.Severity = contract.DiagnosticSeverityWarning
		diagnostic.Message = "required fixture context is missing"
		diagnostic.MissingContext = &contract.MissingContextDetail{
			Context:   missing.Context,
			Reference: missing.Reference,
		}
		return diagnostic
	}
	var unsupported *contract.UnsupportedCapabilityError
	if errors.As(err, &unsupported) {
		diagnostic.Severity = contract.DiagnosticSeverityWarning
		diagnostic.Message = "evaluation requires unsupported semantics"
		diagnostic.UnsupportedCapability = &contract.UnsupportedCapabilityDetail{Capability: unsupported.Capability}
	}
	return diagnostic
}

func terminalReason(trace []contract.TraceStep) contract.ReasonCode {
	for i := len(trace) - 1; i >= 0; i-- {
		if trace[i].Terminal {
			return trace[i].ReasonCode
		}
	}
	return contract.ReasonCodeInternalError
}

func terminalSource(evaluation contract.WebhookEvaluation) string {
	for i := len(evaluation.Trace) - 1; i >= 0; i-- {
		if evaluation.Trace[i].Terminal {
			return evaluation.Trace[i].SourcePath
		}
	}
	return evaluation.SourcePath
}

func ensureTerminalIndeterminate(evaluation *contract.WebhookEvaluation, reason contract.ReasonCode) {
	for i := len(evaluation.Trace) - 1; i >= 0; i-- {
		if evaluation.Trace[i].Terminal {
			evaluation.Trace[i].Result = contract.TraceResultIndeterminate
			return
		}
	}
	evaluation.Trace = append(evaluation.Trace, contract.TraceStep{
		Stage:        "snapshotEvaluation",
		Sequence:     len(evaluation.Trace),
		SourcePath:   evaluation.SourcePath,
		InputSummary: contract.InputSummary{},
		Result:       contract.TraceResultIndeterminate,
		ReasonCode:   reason,
		Terminal:     true,
	})
}

func cloneTrace(input []contract.TraceStep) []contract.TraceStep {
	if input == nil {
		return nil
	}
	trace := make([]contract.TraceStep, len(input))
	copy(trace, input)
	return trace
}

func resequence(trace []contract.TraceStep) {
	for i := range trace {
		trace[i].Sequence = i
	}
}

func outcome(value contract.Outcome) *contract.Outcome {
	return &value
}
