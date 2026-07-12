package contract

// ReasonCode is a stable machine-readable explanation for a trace step or diagnostic.
// Human-readable wording belongs in Diagnostic.Message and is not part of the registry.
type ReasonCode string

const (
	// ReasonCodeCapabilityOutsideProfile identifies semantics outside the selected compatibility profile.
	ReasonCodeCapabilityOutsideProfile ReasonCode = "CAPABILITY_OUTSIDE_PROFILE"
	// ReasonCodeEvaluationProblemDiscarded identifies a previously pending evaluation problem that no longer controls the result.
	ReasonCodeEvaluationProblemDiscarded ReasonCode = "EVALUATION_PROBLEM_DISCARDED"
	// ReasonCodeEvaluationProblemPending identifies an evaluation problem whose terminal effect is not yet known.
	ReasonCodeEvaluationProblemPending ReasonCode = "EVALUATION_PROBLEM_PENDING"
	// ReasonCodeInternalError identifies a failure of an internal invariant or operation.
	ReasonCodeInternalError ReasonCode = "INTERNAL_ERROR"
	// ReasonCodeInvalidInput identifies input that does not satisfy the evaluation contract.
	ReasonCodeInvalidInput ReasonCode = "INVALID_INPUT"
	// ReasonCodeKubernetesEvaluationError identifies an error returned by Kubernetes evaluation semantics.
	ReasonCodeKubernetesEvaluationError ReasonCode = "KUBERNETES_EVALUATION_ERROR"
	// ReasonCodeMatchConditionsTrue identifies that every configured match condition evaluated to true.
	ReasonCodeMatchConditionsTrue ReasonCode = "MATCH_CONDITIONS_TRUE"
	// ReasonCodeMatchConditionTrue identifies a match condition that evaluated to true.
	ReasonCodeMatchConditionTrue ReasonCode = "MATCH_CONDITION_TRUE"
	// ReasonCodeNamespaceContextMissing identifies required namespace fixture context that was not supplied.
	ReasonCodeNamespaceContextMissing ReasonCode = "NAMESPACE_CONTEXT_MISSING"
	// ReasonCodeRuleMatch identifies a rule that matched the admission request.
	ReasonCodeRuleMatch ReasonCode = "RULE_MATCH"
	// ReasonCodeStageNotRun identifies a routing stage that was never evaluated.
	ReasonCodeStageNotRun ReasonCode = "STAGE_NOT_RUN"
)

// ReasonDisposition identifies how a registered reason participates in trace state.
type ReasonDisposition string

const (
	// ReasonDispositionCompleted identifies an observed, non-deferred trace fact.
	ReasonDispositionCompleted ReasonDisposition = "completed"
	// ReasonDispositionPending identifies a problem that may still affect the terminal result.
	ReasonDispositionPending ReasonDisposition = "pending"
	// ReasonDispositionDiscarded identifies a pending problem that was later made non-controlling.
	ReasonDispositionDiscarded ReasonDisposition = "discarded"
	// ReasonDispositionNotRun identifies a stage that was never evaluated.
	ReasonDispositionNotRun ReasonDisposition = "not-run"
)

// IsValid reports whether disposition is part of the reason registry vocabulary.
func (disposition ReasonDisposition) IsValid() bool {
	switch disposition {
	case ReasonDispositionCompleted,
		ReasonDispositionPending,
		ReasonDispositionDiscarded,
		ReasonDispositionNotRun:
		return true
	default:
		return false
	}
}

// ReasonDefinition associates a stable reason code with its trace disposition.
// It intentionally contains no display message, so wording can change independently.
type ReasonDefinition struct {
	Code        ReasonCode
	Disposition ReasonDisposition
}

var reasonCodeRegistry = [...]ReasonDefinition{
	{Code: ReasonCodeCapabilityOutsideProfile, Disposition: ReasonDispositionCompleted},
	{Code: ReasonCodeEvaluationProblemDiscarded, Disposition: ReasonDispositionDiscarded},
	{Code: ReasonCodeEvaluationProblemPending, Disposition: ReasonDispositionPending},
	{Code: ReasonCodeInternalError, Disposition: ReasonDispositionCompleted},
	{Code: ReasonCodeInvalidInput, Disposition: ReasonDispositionCompleted},
	{Code: ReasonCodeKubernetesEvaluationError, Disposition: ReasonDispositionCompleted},
	{Code: ReasonCodeMatchConditionsTrue, Disposition: ReasonDispositionCompleted},
	{Code: ReasonCodeMatchConditionTrue, Disposition: ReasonDispositionCompleted},
	{Code: ReasonCodeNamespaceContextMissing, Disposition: ReasonDispositionCompleted},
	{Code: ReasonCodeRuleMatch, Disposition: ReasonDispositionCompleted},
	{Code: ReasonCodeStageNotRun, Disposition: ReasonDispositionNotRun},
}

// ReasonCodeRegistry returns the deterministic reason registry as a caller-owned copy.
func ReasonCodeRegistry() []ReasonDefinition {
	registry := make([]ReasonDefinition, len(reasonCodeRegistry))
	copy(registry, reasonCodeRegistry[:])
	return registry
}

// RegisteredReasonCodes returns stable reason codes in deterministic registry order.
func RegisteredReasonCodes() []ReasonCode {
	codes := make([]ReasonCode, len(reasonCodeRegistry))
	for i, definition := range reasonCodeRegistry {
		codes[i] = definition.Code
	}
	return codes
}

// IsRegistered reports whether code belongs to the stable reason registry.
func (code ReasonCode) IsRegistered() bool {
	_, ok := reasonDefinition(code)
	return ok
}

func reasonDefinition(code ReasonCode) (ReasonDefinition, bool) {
	for _, definition := range reasonCodeRegistry {
		if definition.Code == code {
			return definition, true
		}
	}
	return ReasonDefinition{}, false
}
