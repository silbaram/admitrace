package routing

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	"github.com/silbaram/admitrace/internal/normalize"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// PreCELStatus identifies whether pre-CEL routing skipped, produced a
// continuation, or surfaced a deferred evaluation problem.
type PreCELStatus string

const (
	// PreCELStatusSkipped identifies a webhook excluded before CEL evaluation.
	PreCELStatusSkipped PreCELStatus = "skipped"
	// PreCELStatusContinue identifies an invocation ready for matchConditions.
	PreCELStatusContinue PreCELStatus = "continue"
	// PreCELStatusProblem identifies an applicable webhook whose buffered problem controls evaluation.
	PreCELStatusProblem PreCELStatus = "problem"
)

// Invocation identifies the resource selected by Exact or Equivalent matching.
type Invocation struct {
	Resource         schema.GroupVersionResource
	Subresource      string
	Kind             schema.GroupVersionKind
	MatchedRuleIndex int
	Equivalent       bool
}

// Continuation is the boundary consumed by the later matchConditions stage.
type Continuation struct {
	Invocation Invocation
}

// EquivalentResourceLookup is the fixture capability consumed by pre-CEL routing.
type EquivalentResourceLookup interface {
	Lookup(contract.ResourceReference) ([]contract.EquivalentResource, error)
}

// EquivalentRuleEvaluation records one rule and equivalent candidate pair.
type EquivalentRuleEvaluation struct {
	RuleIndex       int
	EquivalentIndex int
	Resource        schema.GroupVersionResource
	Matched         bool
}

// EquivalentRulesResult records the ordered fallback lookup and matching result.
type EquivalentRulesResult struct {
	LookupPerformed bool
	Matched         bool
	Evaluations     []EquivalentRuleEvaluation
	Invocation      *Invocation
}

// PreCELResult contains pre-CEL routing state and trace. An outcome is present
// only for a determinate skip; a continuation leaves final outcome to CEL.
type PreCELResult struct {
	Status       PreCELStatus
	Outcome      *contract.Outcome
	Continuation *Continuation
	Trace        []contract.TraceStep
	Exact        ExactRulesResult
	Equivalent   *EquivalentRulesResult
	Err          error
}

type pendingSelector struct {
	traceIndex int
	stage      string
	result     SelectorResult
}

// ShouldCallHookPreCEL follows the Kubernetes 1.36 dispatch precheck and
// ShouldCallHook ordering through rule applicability, stopping before CEL.
func ShouldCallHookPreCEL(
	webhook normalize.NormalizedWebhook,
	request normalize.RequestContext,
	namespaces fixture.NamespaceProvider,
	equivalents EquivalentResourceLookup,
) PreCELResult {
	result := PreCELResult{Trace: make([]contract.TraceStep, 0, 5)}
	if kube136.IsExemptAdmissionConfigurationResource(request.Kind) {
		result.Trace = append(result.Trace, traceStep(
			"admissionConfigurationExemption",
			requestPath(),
			contract.InputSummary{"group": request.Kind.Group, "version": request.Kind.Version, "kind": request.Kind.Kind},
			contract.TraceResultNoMatch,
			contract.ReasonCodeAdmissionConfigurationExcluded,
			true,
		))
		return skippedResult(result)
	}

	pending := make([]pendingSelector, 0, 2)
	namespaceResult := MatchNamespaceSelector(webhook, request, namespaces).SelectorResult
	if appendSelector(&result, "namespaceSelector", namespaceResult, namespaceSummary(request), &pending) {
		return skippedResult(result)
	}

	objectResult := MatchObjectSelector(webhook, request).SelectorResult
	if appendSelector(&result, "objectSelector", objectResult, objectSummary(request), &pending) {
		discardPending(&result, pending)
		return skippedResult(result)
	}

	result.Exact = MatchExactRules(webhook, request)
	exactStep := traceStep("exactRules", result.Exact.SourcePath, ruleSummary(request), traceRuleResult(result.Exact.Matched), result.Exact.ReasonCode, false)
	result.Trace = append(result.Trace, exactStep)
	if result.Exact.Matched {
		invocation := Invocation{
			Resource:         request.Resource,
			Subresource:      request.Subresource,
			Kind:             request.Kind,
			MatchedRuleIndex: result.Exact.MatchedRuleIndex,
		}
		return applicableResult(result, pending, invocation)
	}

	if webhook.MatchPolicy != admissionregistrationv1.Equivalent {
		discardPending(&result, pending)
		result.Trace[len(result.Trace)-1].Terminal = true
		return skippedResult(result)
	}

	equivalentResult, err := matchEquivalentRules(webhook, request, equivalents)
	result.Equivalent = &equivalentResult
	if err != nil {
		traceResult, reason := equivalenceProblemTrace(err)
		result.Trace = append(result.Trace, traceStep("equivalentRules", equivalencePath(), equivalenceSummary(request, equivalentResult), traceResult, reason, true))
		result.Status = PreCELStatusProblem
		result.Err = err
		sequenceTrace(result.Trace)
		return result
	}

	reason := contract.ReasonCodeRuleNoMatch
	traceResult := contract.TraceResultNoMatch
	if equivalentResult.Matched {
		reason = contract.ReasonCodeRuleMatch
		traceResult = contract.TraceResultMatch
	}
	result.Trace = append(result.Trace, traceStep("equivalentRules", equivalencePath(), equivalenceSummary(request, equivalentResult), traceResult, reason, false))
	if !equivalentResult.Matched {
		discardPending(&result, pending)
		result.Trace[len(result.Trace)-1].Terminal = true
		return skippedResult(result)
	}
	return applicableResult(result, pending, *equivalentResult.Invocation)
}

func appendSelector(
	result *PreCELResult,
	stage string,
	selector SelectorResult,
	summary contract.InputSummary,
	pending *[]pendingSelector,
) bool {
	switch selector.Outcome {
	case SelectorOutcomeMatch:
		result.Trace = append(result.Trace, traceStep(stage, selector.SourcePath, summary, contract.TraceResultMatch, selector.ReasonCode, false))
	case SelectorOutcomeNoMatch:
		result.Trace = append(result.Trace, traceStep(stage, selector.SourcePath, summary, contract.TraceResultNoMatch, selector.ReasonCode, true))
		return true
	default:
		traceResult := contract.TraceResultError
		if selector.Outcome == SelectorOutcomeMissingContext {
			traceResult = contract.TraceResultIndeterminate
		}
		step := traceStep(stage, selector.SourcePath, summary, traceResult, contract.ReasonCodeEvaluationProblemPending, false)
		step.Pending = true
		result.Trace = append(result.Trace, step)
		*pending = append(*pending, pendingSelector{traceIndex: len(result.Trace) - 1, stage: stage, result: selector})
	}
	return false
}

func applicableResult(result PreCELResult, pending []pendingSelector, invocation Invocation) PreCELResult {
	if len(pending) == 0 {
		result.Status = PreCELStatusContinue
		result.Continuation = &Continuation{Invocation: invocation}
		sequenceTrace(result.Trace)
		return result
	}

	controlling := pending[0]
	for _, problem := range pending[1:] {
		discardTraceStep(&result.Trace[problem.traceIndex])
	}
	traceResult := contract.TraceResultError
	if controlling.result.Outcome == SelectorOutcomeMissingContext {
		traceResult = contract.TraceResultIndeterminate
	}
	result.Trace = append(result.Trace, traceStep(
		controlling.stage+"Problem",
		controlling.result.SourcePath,
		contract.InputSummary{"bufferedAtSequence": strconv.Itoa(controlling.traceIndex)},
		traceResult,
		controlling.result.ReasonCode,
		true,
	))
	result.Status = PreCELStatusProblem
	result.Err = controlling.result.Err
	sequenceTrace(result.Trace)
	return result
}

func skippedResult(result PreCELResult) PreCELResult {
	outcome := contract.OutcomeSkipped
	result.Status = PreCELStatusSkipped
	result.Outcome = &outcome
	sequenceTrace(result.Trace)
	return result
}

func discardPending(result *PreCELResult, pending []pendingSelector) {
	for _, problem := range pending {
		discardTraceStep(&result.Trace[problem.traceIndex])
	}
}

func discardTraceStep(step *contract.TraceStep) {
	step.Pending = false
	step.Discarded = true
	step.ReasonCode = contract.ReasonCodeEvaluationProblemDiscarded
}

func matchEquivalentRules(
	webhook normalize.NormalizedWebhook,
	request normalize.RequestContext,
	mapper EquivalentResourceLookup,
) (EquivalentRulesResult, error) {
	result := EquivalentRulesResult{LookupPerformed: true, Evaluations: make([]EquivalentRuleEvaluation, 0)}
	if mapper == nil {
		return result, &contract.InternalError{Operation: "lookup equivalent resources", Err: errors.New("equivalence mapper is nil")}
	}
	candidates, err := mapper.Lookup(contract.ResourceReference{
		GVR:         metav1.GroupVersionResource(request.Resource),
		Subresource: request.Subresource,
	})
	if err != nil {
		return result, normalizeEquivalenceLookupError(err)
	}

	for ruleIndex, rule := range webhook.Rules {
		for equivalentIndex, candidate := range candidates {
			resource := schema.GroupVersionResource(candidate.GVR)
			// Kubernetes already evaluated the requested GVR during the Exact pass.
			if resource == request.Resource {
				continue
			}
			matched := kube136.ExactRuleMatches(
				compatibilityRule(rule),
				request.Operation,
				resource,
				candidate.Subresource,
				request.Scope == contract.RequestScopeNamespaced,
			)
			result.Evaluations = append(result.Evaluations, EquivalentRuleEvaluation{
				RuleIndex:       ruleIndex,
				EquivalentIndex: equivalentIndex,
				Resource:        resource,
				Matched:         matched,
			})
			if matched {
				invocation := Invocation{
					Resource:         resource,
					Subresource:      candidate.Subresource,
					Kind:             schema.GroupVersionKind(candidate.GVK),
					MatchedRuleIndex: ruleIndex,
					Equivalent:       true,
				}
				result.Matched = true
				result.Invocation = &invocation
				return result, nil
			}
		}
	}
	return result, nil
}

func traceStep(
	stage string,
	sourcePath string,
	summary contract.InputSummary,
	result contract.TraceResult,
	reason contract.ReasonCode,
	terminal bool,
) contract.TraceStep {
	return contract.TraceStep{
		Stage:        stage,
		SourcePath:   sourcePath,
		InputSummary: summary,
		Result:       result,
		ReasonCode:   reason,
		Terminal:     terminal,
	}
}

func namespaceSummary(request normalize.RequestContext) contract.InputSummary {
	return contract.InputSummary{
		"namespace": request.Namespace,
		"operation": string(request.Operation),
		"resource":  resourceName(request.Resource, request.Subresource),
		"scope":     string(request.Scope),
	}
}

func objectSummary(request normalize.RequestContext) contract.InputSummary {
	return contract.InputSummary{
		"objectState":    string(request.Object.State),
		"oldObjectState": string(request.OldObject.State),
		"operation":      string(request.Operation),
	}
}

func ruleSummary(request normalize.RequestContext) contract.InputSummary {
	return contract.InputSummary{
		"operation": string(request.Operation),
		"resource":  resourceName(request.Resource, request.Subresource),
		"scope":     string(request.Scope),
	}
}

func equivalenceSummary(request normalize.RequestContext, result EquivalentRulesResult) contract.InputSummary {
	summary := contract.InputSummary{
		"evaluations":     strconv.Itoa(len(result.Evaluations)),
		"operation":       string(request.Operation),
		"requestResource": resourceName(request.Resource, request.Subresource),
	}
	if result.Invocation != nil && len(result.Evaluations) > 0 {
		matched := result.Evaluations[len(result.Evaluations)-1]
		summary["matchedEquivalentIndex"] = strconv.Itoa(matched.EquivalentIndex)
		summary["matchedRuleIndex"] = strconv.Itoa(matched.RuleIndex)
		summary["selectedResource"] = resourceName(result.Invocation.Resource, result.Invocation.Subresource)
	}
	return summary
}

func resourceName(resource schema.GroupVersionResource, subresource string) string {
	name := fmt.Sprintf("%s/%s/%s", resource.Group, resource.Version, resource.Resource)
	if subresource != "" {
		name += "/" + subresource
	}
	return name
}

func traceRuleResult(matched bool) contract.TraceResult {
	if matched {
		return contract.TraceResultMatch
	}
	return contract.TraceResultNoMatch
}

func normalizeEquivalenceLookupError(err error) error {
	switch {
	case errors.Is(err, contract.ErrMissingContext),
		errors.Is(err, contract.ErrUnsupportedCapability),
		errors.Is(err, contract.ErrInvalidInput),
		errors.Is(err, contract.ErrKubernetesEvaluation),
		errors.Is(err, contract.ErrInternal):
		return err
	default:
		return &contract.InternalError{Operation: "lookup equivalent resources", Err: err}
	}
}

func equivalenceProblemTrace(err error) (contract.TraceResult, contract.ReasonCode) {
	switch {
	case errors.Is(err, contract.ErrMissingContext):
		return contract.TraceResultIndeterminate, contract.ReasonCodeEquivalenceContextMissing
	case errors.Is(err, contract.ErrUnsupportedCapability):
		return contract.TraceResultUnsupported, contract.ReasonCodeCapabilityOutsideProfile
	case errors.Is(err, contract.ErrInvalidInput):
		return contract.TraceResultError, contract.ReasonCodeInvalidInput
	case errors.Is(err, contract.ErrKubernetesEvaluation):
		return contract.TraceResultError, contract.ReasonCodeKubernetesEvaluationError
	case errors.Is(err, contract.ErrInternal):
		return contract.TraceResultError, contract.ReasonCodeInternalError
	default:
		return contract.TraceResultError, contract.ReasonCodeInternalError
	}
}

func sequenceTrace(trace []contract.TraceStep) {
	for i := range trace {
		trace[i].Sequence = i
	}
}

func requestPath() string {
	return ".request.kind"
}

func equivalencePath() string {
	return ".externalContext.equivalence"
}
