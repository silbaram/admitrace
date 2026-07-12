package routing

import (
	"errors"
	"fmt"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	"github.com/silbaram/admitrace/internal/normalize"
)

// SelectorOutcome identifies the determinate or incomplete result of one
// selector evaluation.
type SelectorOutcome string

const (
	// SelectorOutcomeMatch identifies a selector that matched.
	SelectorOutcomeMatch SelectorOutcome = "match"
	// SelectorOutcomeNoMatch identifies a known selector no-match.
	SelectorOutcomeNoMatch SelectorOutcome = "no-match"
	// SelectorOutcomeEvaluationError identifies a Kubernetes evaluation failure.
	SelectorOutcomeEvaluationError SelectorOutcome = "evaluation-error"
	// SelectorOutcomeMissingContext identifies fixture context needed for evaluation.
	SelectorOutcomeMissingContext SelectorOutcome = "missing-context"
	// SelectorOutcomeInternalError identifies an internal adapter failure.
	SelectorOutcomeInternalError SelectorOutcome = "internal-error"
)

// SelectorResult is a trace-ready selector result with a stable source path
// and reason code. Err is set only for incomplete or error outcomes.
type SelectorResult struct {
	Outcome    SelectorOutcome
	Matched    bool
	SourcePath string
	ReasonCode contract.ReasonCode
	Err        error
}

// NamespaceSelectorResult contains namespace-specific context selection
// details alongside the common selector result.
type NamespaceSelectorResult struct {
	SelectorResult
	ContextMode   kube136.NamespaceContextMode
	ContextSource fixture.NamespaceContextSource
}

// ObjectSelectorResult preserves the normalized object and oldObject states so
// callers can distinguish an absent payload from explicit JSON null.
type ObjectSelectorResult struct {
	SelectorResult
	ObjectState    normalize.ObjectSnapshotState
	OldObjectState normalize.ObjectSnapshotState
}

// MatchNamespaceSelector evaluates one webhook namespace selector using only
// the injected immutable fixture provider for external namespace context.
func MatchNamespaceSelector(
	webhook normalize.NormalizedWebhook,
	request normalize.RequestContext,
	provider fixture.NamespaceProvider,
) NamespaceSelectorResult {
	mode := kube136.NamespaceContextModeFor(
		request.Operation,
		request.Resource,
		request.Subresource,
		request.Scope == contract.RequestScopeNamespaced,
	)
	result := NamespaceSelectorResult{
		ContextMode: mode,
	}
	if mode == kube136.NamespaceContextModeNotRequired {
		result.ContextSource = fixture.NamespaceContextNotRequired
	}

	matched, err := kube136.NamespaceSelectorMatches(webhook.NamespaceSelector, mode, func() (map[string]string, error) {
		context, err := provider.ContextFor(request)
		if err != nil {
			return nil, err
		}
		result.ContextSource = context.Source
		if !context.Required || context.Namespace == nil {
			return nil, &contract.InternalError{
				Operation: "load namespace selector labels",
				Err:       errors.New("required namespace context has no namespace object"),
			}
		}
		return context.Namespace.Labels, nil
	})
	result.SelectorResult = classifySelectorResult(
		matched,
		err,
		webhook.SourcePath+".namespaceSelector",
		contract.ReasonCodeNamespaceSelectorMatch,
		contract.ReasonCodeNamespaceSelectorNoMatch,
		"evaluate namespace selector",
	)
	return result
}

// MatchObjectSelector evaluates one webhook object selector against object and
// oldObject with Kubernetes 1.36 OR semantics.
func MatchObjectSelector(
	webhook normalize.NormalizedWebhook,
	request normalize.RequestContext,
) ObjectSelectorResult {
	matched, err := kube136.ObjectSelectorMatches(
		webhook.ObjectSelector,
		request.Object.Labels,
		request.Object.State == normalize.ObjectSnapshotObject,
		request.OldObject.Labels,
		request.OldObject.State == normalize.ObjectSnapshotObject,
	)
	return ObjectSelectorResult{
		SelectorResult: classifySelectorResult(
			matched,
			err,
			webhook.SourcePath+".objectSelector",
			contract.ReasonCodeObjectSelectorMatch,
			contract.ReasonCodeObjectSelectorNoMatch,
			"evaluate object selector",
		),
		ObjectState:    request.Object.State,
		OldObjectState: request.OldObject.State,
	}
}

func classifySelectorResult(
	matched bool,
	err error,
	sourcePath string,
	matchReason contract.ReasonCode,
	noMatchReason contract.ReasonCode,
	operation string,
) SelectorResult {
	if err == nil {
		if matched {
			return SelectorResult{
				Outcome:    SelectorOutcomeMatch,
				Matched:    true,
				SourcePath: sourcePath,
				ReasonCode: matchReason,
			}
		}
		return SelectorResult{
			Outcome:    SelectorOutcomeNoMatch,
			SourcePath: sourcePath,
			ReasonCode: noMatchReason,
		}
	}

	result := SelectorResult{SourcePath: sourcePath, Err: err}
	switch {
	case errors.Is(err, contract.ErrMissingContext):
		result.Outcome = SelectorOutcomeMissingContext
		result.ReasonCode = contract.ReasonCodeNamespaceContextMissing
	case errors.Is(err, contract.ErrInternal):
		result.Outcome = SelectorOutcomeInternalError
		result.ReasonCode = contract.ReasonCodeInternalError
	default:
		if !errors.Is(err, contract.ErrKubernetesEvaluation) {
			err = &contract.KubernetesEvaluationError{Operation: operation, Err: err}
		}
		result.Outcome = SelectorOutcomeEvaluationError
		result.ReasonCode = contract.ReasonCodeKubernetesEvaluationError
		result.Err = fmt.Errorf("%s: %w", operation, err)
	}
	return result
}
