package contract

import "errors"

// ErrOutcomeRequiresDeterminate indicates an outcome attached to an incomplete evaluation.
var ErrOutcomeRequiresDeterminate = errors.New("outcome requires a determinate evaluation")

// EvaluationPhase identifies the semantic phase represented by a result.
type EvaluationPhase string

const (
	// EvaluationPhaseSnapshotRouting evaluates call eligibility from one supplied snapshot.
	EvaluationPhaseSnapshotRouting EvaluationPhase = "snapshot-routing"
)

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

// ValidateOutcome checks that an incomplete evaluation does not carry an outcome.
func (evaluation WebhookEvaluation) ValidateOutcome() error {
	return validateOutcome(evaluation.Determination, evaluation.Outcome)
}

func validateOutcome(determination Determination, outcome *Outcome) error {
	if determination != DeterminationDeterminate && outcome != nil {
		return ErrOutcomeRequiresDeterminate
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
	ReasonCode   string       `json:"reasonCode"`
	Terminal     bool         `json:"terminal"`
	Pending      bool         `json:"pending"`
	Discarded    bool         `json:"discarded"`
}

// Diagnostic describes a stable, source-addressable evaluation concern.
type Diagnostic struct {
	Code                  string                       `json:"code"`
	Severity              DiagnosticSeverity           `json:"severity"`
	Message               string                       `json:"message"`
	SourcePath            string                       `json:"sourcePath"`
	MissingContext        *MissingContextDetail        `json:"missingContext,omitempty"`
	UnsupportedCapability *UnsupportedCapabilityDetail `json:"unsupportedCapability,omitempty"`
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
