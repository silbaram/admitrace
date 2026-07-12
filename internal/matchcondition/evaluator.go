package matchcondition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	"github.com/silbaram/admitrace/internal/normalize"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authentication/user"
)

// Invocation identifies the resource and kind selected by Exact or Equivalent
// matching for the webhook call.
type Invocation struct {
	Resource    schema.GroupVersionResource
	Subresource string
	Kind        schema.GroupVersionKind
}

// Result contains the standalone matchConditions determination and trace.
type Result struct {
	Determination contract.Determination
	Outcome       *contract.Outcome
	ReasonCode    contract.ReasonCode
	Trace         []contract.TraceStep
	Err           error
}

// Evaluator evaluates matchConditions with Kubernetes 1.36 semantics.
type Evaluator struct {
	compatibility kube136.MatchConditionEvaluator
}

type evaluationOptions struct {
	costBudget int64
}

// Option configures one standalone matchConditions evaluation.
type Option func(*evaluationOptions)

// WithCostBudgetForTest overrides the Kubernetes runtime cost budget. It is
// intended for deterministic cost-limit tests only.
func WithCostBudgetForTest(costBudget int64) Option {
	return func(options *evaluationOptions) {
		options.costBudget = costBudget
	}
}

// NewEvaluator creates a reusable matchConditions evaluator.
func NewEvaluator() Evaluator {
	return Evaluator{compatibility: kube136.NewMatchConditionEvaluator()}
}

// Evaluate compiles and evaluates one normalized webhook's matchConditions.
// The returned trace starts at sequence zero so a later orchestration layer can
// resequence it when appending the pre-CEL trace.
func (e Evaluator) Evaluate(
	ctx context.Context,
	webhook normalize.NormalizedWebhook,
	request normalize.RequestContext,
	invocation Invocation,
	authorizer fixture.Authorizer,
	options ...Option,
) Result {
	settings := evaluationOptions{costBudget: kube136.DefaultMatchConditionCostBudget}
	for _, option := range options {
		option(&settings)
	}

	attributes, err := versionedAttributes(request, invocation)
	if err != nil {
		return internalProblem(webhook.SourcePath+".matchConditions", err)
	}
	raw := e.compatibility.Evaluate(
		ctx,
		webhook.MatchConditions,
		webhook.FailurePolicy,
		attributes,
		authorizer,
		settings.costBudget,
	)
	raw = preserveAuthorizationContext(raw)
	return explainResult(webhook, raw)
}

func preserveAuthorizationContext(raw kube136.MatchConditionsResult) kube136.MatchConditionsResult {
	missingIndex := -1
	unsupportedIndex := -1
	for i := range raw.Observations {
		observation := &raw.Observations[i]
		if observation.Problem != kube136.MatchConditionProblemAuthorization {
			continue
		}
		switch {
		case errors.Is(observation.Err, contract.ErrUnsupportedCapability):
			observation.Problem = kube136.MatchConditionProblemAuthorizationUnsupported
			if unsupportedIndex < 0 {
				unsupportedIndex = i
			}
		case errors.Is(observation.Err, contract.ErrMissingContext):
			observation.Problem = kube136.MatchConditionProblemAuthorizationMissing
			if missingIndex < 0 {
				missingIndex = i
			}
		}
	}
	// An independent false condition is sufficient to skip the webhook even
	// when another authorizer query is missing or unsupported.
	if raw.Decision == kube136.MatchConditionDecisionSkipFalse {
		return raw
	}
	if unsupportedIndex >= 0 {
		raw.Decision = kube136.MatchConditionDecisionUnsupported
		raw.ControllingIndex = unsupportedIndex
		raw.Err = raw.Observations[unsupportedIndex].Err
	} else if missingIndex >= 0 {
		raw.Decision = kube136.MatchConditionDecisionIndeterminate
		raw.ControllingIndex = missingIndex
		raw.Err = raw.Observations[missingIndex].Err
	}
	return raw
}

func versionedAttributes(
	request normalize.RequestContext,
	invocation Invocation,
) (*admission.VersionedAttributes, error) {
	object, err := snapshotObject(request.Object, invocation.Kind)
	if err != nil {
		return nil, fmt.Errorf("build CEL object snapshot: %w", err)
	}
	oldObject, err := snapshotObject(request.OldObject, invocation.Kind)
	if err != nil {
		return nil, fmt.Errorf("build CEL oldObject snapshot: %w", err)
	}

	attributes := admission.NewAttributesRecord(
		object,
		oldObject,
		request.Kind,
		request.Namespace,
		request.Name,
		request.Resource,
		request.Subresource,
		admission.Operation(request.Operation),
		nil,
		request.DryRun,
		requestUser(request),
	)
	return &admission.VersionedAttributes{
		Attributes:         attributes,
		VersionedOldObject: oldObject,
		VersionedObject:    object,
		VersionedKind:      invocation.Kind,
	}, nil
}

func snapshotObject(snapshot normalize.ObjectSnapshot, kind schema.GroupVersionKind) (runtime.Object, error) {
	if snapshot.State != normalize.ObjectSnapshotObject {
		return nil, nil
	}
	var object map[string]any
	if err := json.Unmarshal(snapshot.Raw, &object); err != nil {
		return nil, err
	}
	result := &unstructured.Unstructured{Object: object}
	result.SetGroupVersionKind(kind)
	return result, nil
}

func requestUser(request normalize.RequestContext) *user.DefaultInfo {
	extra := make(map[string][]string, len(request.UserInfo.Extra))
	for key, values := range request.UserInfo.Extra {
		extra[key] = append([]string(nil), values...)
	}
	return &user.DefaultInfo{
		Name:   request.UserInfo.Username,
		UID:    request.UserInfo.UID,
		Groups: append([]string(nil), request.UserInfo.Groups...),
		Extra:  extra,
	}
}

func explainResult(
	webhook normalize.NormalizedWebhook,
	raw kube136.MatchConditionsResult,
) Result {
	trace := make([]contract.TraceStep, 0, len(raw.Observations)+1)
	for _, observation := range raw.Observations {
		trace = append(trace, observationStep(webhook, observation))
	}

	result := Result{Trace: trace}
	switch raw.Decision {
	case kube136.MatchConditionDecisionCall:
		result.Determination = contract.DeterminationDeterminate
		result.Outcome = outcome(contract.OutcomeCalled)
		result.ReasonCode = contract.ReasonCodeMatchConditionsTrue
		result.Trace = append(result.Trace, summaryStep(webhook, raw, contract.TraceResultTrue, result.ReasonCode))
	case kube136.MatchConditionDecisionSkipFalse:
		result.Determination = contract.DeterminationDeterminate
		result.Outcome = outcome(contract.OutcomeSkipped)
		result.ReasonCode = contract.ReasonCodeMatchConditionFalse
		result.Trace = append(result.Trace, summaryStep(webhook, raw, contract.TraceResultFalse, result.ReasonCode))
	case kube136.MatchConditionDecisionReject:
		result.Determination = contract.DeterminationDeterminate
		result.Outcome = outcome(contract.OutcomeRejectedBeforeCall)
		result.ReasonCode, result.Err = controllingProblem(raw)
		result.Trace = append(result.Trace, summaryStep(webhook, raw, contract.TraceResultError, result.ReasonCode))
	case kube136.MatchConditionDecisionSkipError:
		result.Determination = contract.DeterminationDeterminate
		result.Outcome = outcome(contract.OutcomeSkipped)
		result.ReasonCode, result.Err = controllingProblem(raw)
		result.Trace = append(result.Trace, summaryStep(webhook, raw, contract.TraceResultError, result.ReasonCode))
	case kube136.MatchConditionDecisionIndeterminate:
		result.Determination = contract.DeterminationIndeterminate
		result.ReasonCode = contract.ReasonCodeAuthorizationContextMissing
		result.Err = raw.Err
		result.Trace = append(result.Trace, summaryStep(webhook, raw, contract.TraceResultIndeterminate, result.ReasonCode))
	case kube136.MatchConditionDecisionUnsupported:
		result.Determination = contract.DeterminationUnsupported
		result.ReasonCode = contract.ReasonCodeCapabilityOutsideProfile
		result.Err = raw.Err
		result.Trace = append(result.Trace, summaryStep(webhook, raw, contract.TraceResultUnsupported, result.ReasonCode))
	default:
		return internalProblem(webhook.SourcePath+".matchConditions", fmt.Errorf("unknown compatibility decision %q", raw.Decision))
	}
	sequenceTrace(result.Trace)
	return result
}

func observationStep(
	webhook normalize.NormalizedWebhook,
	observation kube136.MatchConditionObservation,
) contract.TraceStep {
	result := contract.TraceResultError
	reason := problemReason(observation.Problem)
	if observation.Problem == kube136.MatchConditionProblemAuthorizationUnsupported {
		result = contract.TraceResultUnsupported
	} else if observation.Problem == kube136.MatchConditionProblemAuthorizationMissing {
		result = contract.TraceResultIndeterminate
	} else if observation.Value != nil {
		if *observation.Value {
			result = contract.TraceResultTrue
			reason = contract.ReasonCodeMatchConditionTrue
		} else {
			result = contract.TraceResultFalse
			reason = contract.ReasonCodeMatchConditionFalse
		}
	}
	return contract.TraceStep{
		Stage:        "matchConditions",
		SourcePath:   fmt.Sprintf("%s.matchConditions[%d]", webhook.SourcePath, observation.Index),
		InputSummary: contract.InputSummary{"name": observation.Name, "failurePolicy": string(webhook.FailurePolicy)},
		Result:       result,
		ReasonCode:   reason,
	}
}

func summaryStep(
	webhook normalize.NormalizedWebhook,
	raw kube136.MatchConditionsResult,
	result contract.TraceResult,
	reason contract.ReasonCode,
) contract.TraceStep {
	summary := contract.InputSummary{
		"count":         fmt.Sprintf("%d", len(webhook.MatchConditions)),
		"failurePolicy": string(webhook.FailurePolicy),
	}
	if raw.ControllingIndex >= 0 && raw.ControllingIndex < len(raw.Observations) {
		observation := raw.Observations[raw.ControllingIndex]
		summary["controllingIndex"] = fmt.Sprintf("%d", observation.Index)
		summary["controllingName"] = observation.Name
	}
	return contract.TraceStep{
		Stage:        "matchConditions",
		SourcePath:   webhook.SourcePath + ".matchConditions",
		InputSummary: summary,
		Result:       result,
		ReasonCode:   reason,
		Terminal:     true,
	}
}

func controllingProblem(raw kube136.MatchConditionsResult) (contract.ReasonCode, error) {
	if raw.ControllingIndex < 0 || raw.ControllingIndex >= len(raw.Observations) {
		return contract.ReasonCodeInternalError, &contract.InternalError{
			Operation: "classify matchConditions result",
			Err:       errors.New("controlling condition is missing"),
		}
	}
	observation := raw.Observations[raw.ControllingIndex]
	reason := problemReason(observation.Problem)
	if observation.Err == nil {
		return reason, nil
	}
	return reason, &contract.KubernetesEvaluationError{
		Operation: "evaluate match condition",
		Err:       observation.Err,
	}
}

func problemReason(problem kube136.MatchConditionProblem) contract.ReasonCode {
	switch problem {
	case kube136.MatchConditionProblemCompile:
		return contract.ReasonCodeCELCompileError
	case kube136.MatchConditionProblemRuntime:
		return contract.ReasonCodeCELRuntimeError
	case kube136.MatchConditionProblemCost:
		return contract.ReasonCodeCELCostBudgetExceeded
	case kube136.MatchConditionProblemAuthorization:
		return contract.ReasonCodeCELAuthorizationError
	case kube136.MatchConditionProblemAuthorizationUnsupported:
		return contract.ReasonCodeCapabilityOutsideProfile
	case kube136.MatchConditionProblemAuthorizationMissing:
		return contract.ReasonCodeAuthorizationContextMissing
	default:
		return contract.ReasonCodeInternalError
	}
}

func sequenceTrace(trace []contract.TraceStep) {
	for i := range trace {
		trace[i].Sequence = i
	}
}

func outcome(value contract.Outcome) *contract.Outcome {
	return &value
}

func internalProblem(sourcePath string, err error) Result {
	problem := &contract.InternalError{Operation: "prepare matchConditions evaluation", Err: err}
	return Result{
		Determination: contract.DeterminationIndeterminate,
		ReasonCode:    contract.ReasonCodeInternalError,
		Trace: []contract.TraceStep{
			{
				Stage:        "matchConditions",
				SourcePath:   sourcePath,
				InputSummary: contract.InputSummary{},
				Result:       contract.TraceResultIndeterminate,
				ReasonCode:   contract.ReasonCodeInternalError,
				Terminal:     true,
			},
		},
		Err: problem,
	}
}
