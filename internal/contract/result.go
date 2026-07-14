package contract

import "fmt"

// EvaluationPhase identifies the semantic phase represented by a result.
type EvaluationPhase string

const (
	// EvaluationPhaseSnapshotRouting evaluates validating call eligibility from one supplied snapshot.
	EvaluationPhaseSnapshotRouting EvaluationPhase = "snapshot-routing"
	// EvaluationPhaseMutatingInitialSnapshot evaluates mutating webhook eligibility
	// before any webhook patch or reinvocation could change the request.
	EvaluationPhaseMutatingInitialSnapshot EvaluationPhase = "mutating-initial-snapshot-eligibility"
)

// IsValid reports whether phase belongs to the evaluation phase vocabulary.
func (phase EvaluationPhase) IsValid() bool {
	return phase == EvaluationPhaseSnapshotRouting || phase == EvaluationPhaseMutatingInitialSnapshot
}

// Determination identifies whether evaluation completed within the supported contract.
type Determination string

const (
	// DeterminationDeterminate indicates that evaluation produced an outcome.
	DeterminationDeterminate Determination = "determinate"
	// DeterminationIndeterminate indicates that required fixture context was unavailable.
	DeterminationIndeterminate Determination = "indeterminate"
	// DeterminationUnsupported indicates that evaluation requires unsupported semantics.
	DeterminationUnsupported Determination = "unsupported"
)

// IsValid reports whether determination belongs to the determination vocabulary.
func (determination Determination) IsValid() bool {
	switch determination {
	case DeterminationDeterminate, DeterminationIndeterminate, DeterminationUnsupported:
		return true
	default:
		return false
	}
}

// Outcome identifies a completed admission webhook routing result.
type Outcome string

const (
	// OutcomeCalled indicates that the webhook was selected by the snapshot routing pipeline.
	OutcomeCalled Outcome = "called"
	// OutcomeSkipped indicates that the webhook was not selected by the snapshot routing pipeline.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeRejectedBeforeCall indicates that failure policy rejects the request before a call.
	OutcomeRejectedBeforeCall Outcome = "rejected-before-call"
)

// IsValid reports whether outcome belongs to the outcome vocabulary.
func (outcome Outcome) IsValid() bool {
	switch outcome {
	case OutcomeCalled, OutcomeSkipped, OutcomeRejectedBeforeCall:
		return true
	default:
		return false
	}
}

// TraceResult identifies the result of one trace step.
type TraceResult string

const (
	// TraceResultMatch indicates a matching selector, rule, or mapping.
	TraceResultMatch TraceResult = "match"
	// TraceResultNoMatch indicates a non-matching selector, rule, or mapping.
	TraceResultNoMatch TraceResult = "no-match"
	// TraceResultTrue indicates a true boolean expression.
	TraceResultTrue TraceResult = "true"
	// TraceResultFalse indicates a false boolean expression.
	TraceResultFalse TraceResult = "false"
	// TraceResultError indicates an evaluation error.
	TraceResultError TraceResult = "error"
	// TraceResultIndeterminate indicates a result blocked by missing context.
	TraceResultIndeterminate TraceResult = "indeterminate"
	// TraceResultUnsupported indicates a result outside supported semantics.
	TraceResultUnsupported TraceResult = "unsupported"
	// TraceResultNotRun indicates a stage that was not executed.
	TraceResultNotRun TraceResult = "not-run"
)

// IsValid reports whether result belongs to the trace result vocabulary.
func (result TraceResult) IsValid() bool {
	switch result {
	case TraceResultMatch,
		TraceResultNoMatch,
		TraceResultTrue,
		TraceResultFalse,
		TraceResultError,
		TraceResultIndeterminate,
		TraceResultUnsupported,
		TraceResultNotRun:
		return true
	default:
		return false
	}
}

// DiagnosticSeverity identifies the importance of a diagnostic.
type DiagnosticSeverity string

const (
	// DiagnosticSeverityInfo identifies informational diagnostics.
	DiagnosticSeverityInfo DiagnosticSeverity = "info"
	// DiagnosticSeverityWarning identifies warning diagnostics.
	DiagnosticSeverityWarning DiagnosticSeverity = "warning"
	// DiagnosticSeverityError identifies error diagnostics.
	DiagnosticSeverityError DiagnosticSeverity = "error"
)

// IsValid reports whether severity belongs to the diagnostic severity vocabulary.
func (severity DiagnosticSeverity) IsValid() bool {
	switch severity {
	case DiagnosticSeverityInfo, DiagnosticSeverityWarning, DiagnosticSeverityError:
		return true
	default:
		return false
	}
}

// EvaluationResult is the versioned canonical result for one Scenario.
type EvaluationResult struct {
	SchemaVersion        string               `json:"schemaVersion"`
	ScenarioID           string               `json:"scenarioId"`
	CompatibilityProfile CompatibilityProfile `json:"compatibilityProfile"`
	EvaluationPhase      EvaluationPhase      `json:"evaluationPhase"`
	ConfigurationKind    ConfigurationKind    `json:"configurationKind"`
	Webhooks             []WebhookEvaluation  `json:"webhooks"`
	Diagnostics          []Diagnostic         `json:"diagnostics"`
}

// Validate checks result vocabulary and the invariants of every nested evaluation.
func (result EvaluationResult) Validate() error {
	if !IsSupportedResultVersion(result.SchemaVersion) {
		return &ValidationError{Field: "schemaVersion", Value: result.SchemaVersion, Err: ErrInvalidEnumValue}
	}
	if !IsSupportedCompatibilityProfile(result.CompatibilityProfile) {
		return &ValidationError{Field: "compatibilityProfile", Value: result.CompatibilityProfile.ID, Err: ErrInvalidEnumValue}
	}
	if !result.ConfigurationKind.IsValid() {
		return &ValidationError{Field: "configurationKind", Value: string(result.ConfigurationKind), Err: ErrInvalidEnumValue}
	}
	if !result.EvaluationPhase.IsValid() {
		return &ValidationError{Field: "evaluationPhase", Value: string(result.EvaluationPhase), Err: ErrInvalidEnumValue}
	}
	if result.ConfigurationKind == ConfigurationKindMutating && result.EvaluationPhase != EvaluationPhaseMutatingInitialSnapshot {
		return &ValidationError{Field: "evaluationPhase", Value: string(result.EvaluationPhase), Err: ErrInvalidEnumValue}
	}
	if result.ConfigurationKind == ConfigurationKindValidating && result.EvaluationPhase != EvaluationPhaseSnapshotRouting {
		return &ValidationError{Field: "evaluationPhase", Value: string(result.EvaluationPhase), Err: ErrInvalidEnumValue}
	}
	for i, evaluation := range result.Webhooks {
		if evaluation.ConfigurationKind != result.ConfigurationKind {
			return &ValidationError{
				Field: fmt.Sprintf("webhooks[%d].configurationKind", i),
				Value: string(evaluation.ConfigurationKind),
				Err:   ErrConfigurationKindMismatch,
			}
		}
		if err := evaluation.Validate(); err != nil {
			return fmt.Errorf("webhooks[%d]: %w", i, err)
		}
	}
	for i, diagnostic := range result.Diagnostics {
		if err := diagnostic.Validate(); err != nil {
			return fmt.Errorf("diagnostics[%d]: %w", i, err)
		}
	}
	return nil
}

// WebhookEvaluation is the ordered canonical result for one configured webhook.
type WebhookEvaluation struct {
	ConfigurationKind ConfigurationKind `json:"configurationKind"`
	WebhookName       string            `json:"webhookName"`
	WebhookIndex      int               `json:"webhookIndex"`
	SourcePath        string            `json:"sourcePath"`
	Determination     Determination     `json:"determination"`
	Outcome           *Outcome          `json:"outcome,omitempty"`
	Trace             []TraceStep       `json:"trace"`
	Diagnostics       []Diagnostic      `json:"diagnostics"`
}

// ValidateOutcome checks the mandatory determination and outcome relationship.
func (evaluation WebhookEvaluation) ValidateOutcome() error {
	return validateEvaluationOutcome(evaluation.Determination, evaluation.Outcome)
}

// Validate checks vocabulary, outcome, diagnostic, and terminal trace invariants.
func (evaluation WebhookEvaluation) Validate() error {
	if !evaluation.Determination.IsValid() {
		return &ValidationError{Field: "determination", Value: string(evaluation.Determination), Err: ErrInvalidEnumValue}
	}
	if err := evaluation.ValidateOutcome(); err != nil {
		return err
	}

	terminalCount := 0
	for i, step := range evaluation.Trace {
		if err := step.Validate(); err != nil {
			return fmt.Errorf("trace[%d]: %w", i, err)
		}
		if step.Terminal {
			terminalCount++
			if err := validateTerminalResult(evaluation.Determination, step.Result); err != nil {
				return fmt.Errorf("trace[%d]: %w", i, err)
			}
		}
	}
	if evaluation.Determination == DeterminationDeterminate {
		switch {
		case terminalCount == 0:
			return &ValidationError{Field: "trace", Err: ErrTerminalTraceRequired}
		case terminalCount > 1:
			return &ValidationError{Field: "trace", Value: fmt.Sprintf("%d terminal steps", terminalCount), Err: ErrMultipleTerminalTraceSteps}
		}
	}
	for i, diagnostic := range evaluation.Diagnostics {
		if err := diagnostic.Validate(); err != nil {
			return fmt.Errorf("diagnostics[%d]: %w", i, err)
		}
	}
	return nil
}

func validateEvaluationOutcome(determination Determination, outcome *Outcome) error {
	if !determination.IsValid() {
		return &ValidationError{Field: "determination", Value: string(determination), Err: ErrInvalidEnumValue}
	}
	if determination == DeterminationDeterminate {
		if outcome == nil {
			return &ValidationError{Field: "outcome", Err: ErrDeterminateRequiresOutcome}
		}
		if !outcome.IsValid() {
			return &ValidationError{Field: "outcome", Value: string(*outcome), Err: ErrInvalidEnumValue}
		}
		return nil
	}
	if outcome != nil {
		return &ValidationError{Field: "outcome", Value: string(*outcome), Err: ErrOutcomeRequiresDeterminate}
	}
	return nil
}

// InputSummary is a redacted, normalized summary of inputs used by a trace step.
type InputSummary map[string]string

// TraceStep records one ordered stage of webhook routing evaluation.
type TraceStep struct {
	Stage        string       `json:"stage"`
	Sequence     int          `json:"sequence"`
	SourcePath   string       `json:"sourcePath"`
	InputSummary InputSummary `json:"inputSummary"`
	Result       TraceResult  `json:"result"`
	ReasonCode   ReasonCode   `json:"reasonCode"`
	Terminal     bool         `json:"terminal"`
	Pending      bool         `json:"pending"`
	Discarded    bool         `json:"discarded"`
}

// Validate checks trace vocabulary and makes pending, discarded, and not-run states unambiguous.
func (step TraceStep) Validate() error {
	if !step.Result.IsValid() {
		return &ValidationError{Field: "result", Value: string(step.Result), Err: ErrInvalidEnumValue}
	}
	definition, ok := reasonDefinition(step.ReasonCode)
	if !ok {
		return &ValidationError{Field: "reasonCode", Value: string(step.ReasonCode), Err: ErrUnregisteredReasonCode}
	}
	if step.Pending && step.Discarded {
		return &ValidationError{Field: "pending/discarded", Err: ErrInvalidTraceState}
	}
	if step.Terminal && (step.Pending || step.Discarded || step.Result == TraceResultNotRun) {
		return &ValidationError{Field: "terminal", Err: ErrInvalidTraceState}
	}

	validState := false
	switch definition.Disposition {
	case ReasonDispositionCompleted:
		validState = !step.Pending && !step.Discarded && step.Result != TraceResultNotRun
	case ReasonDispositionPending:
		validState = step.Pending && !step.Discarded && isDeferredProblemResult(step.Result)
	case ReasonDispositionDiscarded:
		validState = !step.Pending && step.Discarded && isDeferredProblemResult(step.Result)
	case ReasonDispositionNotRun:
		validState = !step.Pending && !step.Discarded && step.Result == TraceResultNotRun
	}
	if !validState {
		return &ValidationError{Field: "traceState", Value: string(definition.Disposition), Err: ErrInvalidTraceState}
	}
	return nil
}

func isDeferredProblemResult(result TraceResult) bool {
	return result == TraceResultError || result == TraceResultIndeterminate
}

// Diagnostic describes a stable, source-addressable evaluation concern.
type Diagnostic struct {
	Code                  ReasonCode                   `json:"code"`
	Severity              DiagnosticSeverity           `json:"severity"`
	Message               string                       `json:"message"`
	SourcePath            string                       `json:"sourcePath"`
	MissingContext        *MissingContextDetail        `json:"missingContext,omitempty"`
	UnsupportedCapability *UnsupportedCapabilityDetail `json:"unsupportedCapability,omitempty"`
}

// Validate checks diagnostic vocabulary independently from display wording.
func (diagnostic Diagnostic) Validate() error {
	if !diagnostic.Code.IsRegistered() {
		return &ValidationError{Field: "code", Value: string(diagnostic.Code), Err: ErrUnregisteredReasonCode}
	}
	if !diagnostic.Severity.IsValid() {
		return &ValidationError{Field: "severity", Value: string(diagnostic.Severity), Err: ErrInvalidEnumValue}
	}
	return nil
}

func validateTerminalResult(determination Determination, result TraceResult) error {
	valid := false
	switch determination {
	case DeterminationDeterminate:
		valid = result != TraceResultIndeterminate && result != TraceResultUnsupported && result != TraceResultNotRun
	case DeterminationIndeterminate:
		valid = result == TraceResultIndeterminate
	case DeterminationUnsupported:
		valid = result == TraceResultUnsupported
	}
	if valid {
		return nil
	}
	return &ValidationError{Field: "terminal.result", Value: string(result), Err: ErrInvalidTraceState}
}

// MissingContextDetail identifies fixture context required to complete evaluation.
type MissingContextDetail struct {
	Context   string `json:"context"`
	Reference string `json:"reference,omitempty"`
}

// UnsupportedCapabilityDetail identifies semantics outside the compatibility profile.
type UnsupportedCapabilityDetail struct {
	Capability string `json:"capability"`
	Detail     string `json:"detail,omitempty"`
}
