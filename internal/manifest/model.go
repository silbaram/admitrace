package manifest

import (
	"fmt"

	"github.com/silbaram/admitrace/internal/contract"
)

// ExplanationSchemaVersion is the version of the resource-mode envelope.
const ExplanationSchemaVersion = "admitrace.manifest-explanation/v1alpha1"

// SourceKind identifies how a manifest or configuration entered the adapter.
type SourceKind string

const (
	// SourceKindFile identifies a document read from one explicit file.
	SourceKindFile SourceKind = "file"
	// SourceKindStdin identifies a document read from standard input.
	SourceKindStdin SourceKind = "stdin"
	// SourceKindDirectoryEntry identifies a document discovered in a directory.
	SourceKindDirectoryEntry SourceKind = "directory-entry"
	// SourceKindCluster identifies an object read from an explicitly selected cluster context.
	SourceKindCluster SourceKind = "cluster"
)

// IsValid reports whether kind belongs to the source vocabulary.
func (kind SourceKind) IsValid() bool {
	switch kind {
	case SourceKindFile, SourceKindStdin, SourceKindDirectoryEntry, SourceKindCluster:
		return true
	default:
		return false
	}
}

// Source identifies a logical input without exposing an absolute path or credentials.
type Source struct {
	Kind          SourceKind `json:"kind"`
	Label         string     `json:"label"`
	DocumentIndex int        `json:"documentIndex,omitempty"`
}

// Validate checks the source vocabulary and document provenance invariants.
func (source Source) Validate() error {
	if !source.Kind.IsValid() {
		return &contract.ValidationError{Field: "kind", Value: string(source.Kind), Err: contract.ErrInvalidEnumValue}
	}
	if source.Label == "" {
		return &contract.ValidationError{Field: "label", Err: contract.ErrInvalidInput}
	}
	if source.Kind == SourceKindCluster {
		if source.DocumentIndex != 0 {
			return &contract.ValidationError{Field: "documentIndex", Value: fmt.Sprint(source.DocumentIndex), Err: contract.ErrInvalidInput}
		}
		return nil
	}
	if source.DocumentIndex < 1 {
		return &contract.ValidationError{Field: "documentIndex", Value: fmt.Sprint(source.DocumentIndex), Err: contract.ErrInvalidInput}
	}
	return nil
}

// ProfileMatchStatus identifies how the selected compatibility profile was established.
type ProfileMatchStatus string

const (
	// ProfileMatchDeclared identifies the fixed profile used without cluster access.
	ProfileMatchDeclared ProfileMatchStatus = "declared"
	// ProfileMatchVerified identifies an exact connected-server profile match.
	ProfileMatchVerified ProfileMatchStatus = "verified"
	// ProfileMatchMismatch identifies a connected-server version that cannot use the selected profile.
	ProfileMatchMismatch ProfileMatchStatus = "mismatch"
)

// IsValid reports whether status belongs to the profile-match vocabulary.
func (status ProfileMatchStatus) IsValid() bool {
	switch status {
	case ProfileMatchDeclared, ProfileMatchVerified, ProfileMatchMismatch:
		return true
	default:
		return false
	}
}

// ProfileMatch records the selected profile and optional connected-server evidence.
type ProfileMatch struct {
	Status                    ProfileMatchStatus            `json:"status"`
	Profile                   contract.CompatibilityProfile `json:"profile"`
	ObservedKubernetesVersion string                        `json:"observedKubernetesVersion,omitempty"`
}

// Validate checks profile status and exact-version evidence.
func (match ProfileMatch) Validate() error {
	if !match.Status.IsValid() {
		return &contract.ValidationError{Field: "status", Value: string(match.Status), Err: contract.ErrInvalidEnumValue}
	}
	if !contract.IsSupportedCompatibilityProfile(match.Profile) {
		return &contract.ValidationError{Field: "profile", Value: match.Profile.ID, Err: contract.ErrUnsupportedCapability}
	}
	if match.Status == ProfileMatchDeclared && match.ObservedKubernetesVersion != "" {
		return &contract.ValidationError{Field: "observedKubernetesVersion", Value: match.ObservedKubernetesVersion, Err: contract.ErrInvalidInput}
	}
	if match.Status != ProfileMatchDeclared && match.ObservedKubernetesVersion == "" {
		return &contract.ValidationError{Field: "observedKubernetesVersion", Err: contract.ErrInvalidInput}
	}
	return nil
}

// CompletenessStatus identifies how one evaluator context dependency was handled.
type CompletenessStatus string

const (
	// CompletenessProvided identifies context supplied explicitly by the user.
	CompletenessProvided CompletenessStatus = "provided"
	// CompletenessHydrated identifies context read from an explicitly selected cluster.
	CompletenessHydrated CompletenessStatus = "hydrated"
	// CompletenessNotRequired identifies context unnecessary for the current evaluation.
	CompletenessNotRequired CompletenessStatus = "not-required"
	// CompletenessMissing identifies required context that was not supplied.
	CompletenessMissing CompletenessStatus = "missing"
	// CompletenessForbidden identifies context that could not be read due to authorization.
	CompletenessForbidden CompletenessStatus = "forbidden"
	// CompletenessUnsupported identifies context outside the adapter safety or compatibility profile.
	CompletenessUnsupported CompletenessStatus = "unsupported"
)

// IsValid reports whether status belongs to the completeness vocabulary.
func (status CompletenessStatus) IsValid() bool {
	switch status {
	case CompletenessProvided,
		CompletenessHydrated,
		CompletenessNotRequired,
		CompletenessMissing,
		CompletenessForbidden,
		CompletenessUnsupported:
		return true
	default:
		return false
	}
}

// ContextStatus records the completeness state and its logical source.
type ContextStatus struct {
	Status      CompletenessStatus `json:"status"`
	SourceLabel string             `json:"sourceLabel,omitempty"`
}

// Validate checks the completeness vocabulary and source relationship.
func (status ContextStatus) Validate() error {
	if !status.Status.IsValid() {
		return &contract.ValidationError{Field: "status", Value: string(status.Status), Err: contract.ErrInvalidEnumValue}
	}
	hasSource := status.SourceLabel != ""
	requiresSource := status.Status == CompletenessProvided || status.Status == CompletenessHydrated
	if hasSource != requiresSource {
		return &contract.ValidationError{Field: "sourceLabel", Value: status.SourceLabel, Err: contract.ErrInvalidInput}
	}
	return nil
}

// ContextCompleteness records every external context dimension used by the evaluator.
type ContextCompleteness struct {
	Configuration ContextStatus `json:"configuration"`
	Discovery     ContextStatus `json:"discovery"`
	Namespace     ContextStatus `json:"namespace"`
	Identity      ContextStatus `json:"identity"`
	Equivalence   ContextStatus `json:"equivalence"`
	Authorization ContextStatus `json:"authorization"`
}

// Validate checks all completeness dimensions in stable field order.
func (completeness ContextCompleteness) Validate() error {
	checks := []struct {
		field  string
		status ContextStatus
	}{
		{field: "configuration", status: completeness.Configuration},
		{field: "discovery", status: completeness.Discovery},
		{field: "namespace", status: completeness.Namespace},
		{field: "identity", status: completeness.Identity},
		{field: "equivalence", status: completeness.Equivalence},
		{field: "authorization", status: completeness.Authorization},
	}
	for _, check := range checks {
		if err := check.status.Validate(); err != nil {
			return fmt.Errorf("%s: %w", check.field, err)
		}
	}
	return nil
}

// DiagnosticCode is a stable machine-readable adapter diagnostic.
type DiagnosticCode string

const (
	// DiagnosticCodeIncomplete identifies an adapter result with unavailable required context.
	DiagnosticCodeIncomplete DiagnosticCode = "ADAPTER_INCOMPLETE"
	// DiagnosticCodeProfileMismatch identifies an exact Kubernetes profile mismatch.
	DiagnosticCodeProfileMismatch DiagnosticCode = "PROFILE_MISMATCH"
	// DiagnosticCodeIdentityContextMissing identifies missing explicit admission identity.
	DiagnosticCodeIdentityContextMissing DiagnosticCode = "IDENTITY_CONTEXT_MISSING"
	// DiagnosticCodeSnapshotRefused identifies an exact-copy snapshot rejected by policy.
	DiagnosticCodeSnapshotRefused DiagnosticCode = "SNAPSHOT_REFUSED"
	// DiagnosticCodeUnsupportedOperation identifies a non-CREATE adapter request.
	DiagnosticCodeUnsupportedOperation DiagnosticCode = "UNSUPPORTED_OPERATION"
)

// IsValid reports whether code belongs to the adapter diagnostic vocabulary.
func (code DiagnosticCode) IsValid() bool {
	switch code {
	case DiagnosticCodeIncomplete,
		DiagnosticCodeProfileMismatch,
		DiagnosticCodeIdentityContextMissing,
		DiagnosticCodeSnapshotRefused,
		DiagnosticCodeUnsupportedOperation:
		return true
	default:
		return false
	}
}

// Diagnostic describes a stable, source-addressable adapter concern.
type Diagnostic struct {
	Code          DiagnosticCode              `json:"code"`
	Severity      contract.DiagnosticSeverity `json:"severity"`
	Message       string                      `json:"message"`
	SourceLabel   string                      `json:"sourceLabel,omitempty"`
	DocumentIndex int                         `json:"documentIndex,omitempty"`
}

// Validate checks adapter diagnostic vocabulary and provenance.
func (diagnostic Diagnostic) Validate() error {
	if !diagnostic.Code.IsValid() {
		return &contract.ValidationError{Field: "code", Value: string(diagnostic.Code), Err: contract.ErrUnregisteredReasonCode}
	}
	if !diagnostic.Severity.IsValid() {
		return &contract.ValidationError{Field: "severity", Value: string(diagnostic.Severity), Err: contract.ErrInvalidEnumValue}
	}
	if diagnostic.Message == "" {
		return &contract.ValidationError{Field: "message", Err: contract.ErrInvalidInput}
	}
	if diagnostic.DocumentIndex < 0 || (diagnostic.DocumentIndex > 0 && diagnostic.SourceLabel == "") {
		return &contract.ValidationError{Field: "documentIndex", Value: fmt.Sprint(diagnostic.DocumentIndex), Err: contract.ErrInvalidInput}
	}
	return nil
}

// ConfigurationEvaluation associates one configuration source with its unchanged evaluator result.
type ConfigurationEvaluation struct {
	Configuration Source                    `json:"configuration"`
	Result        contract.EvaluationResult `json:"result"`
}

// ManifestExplanation is the resource-mode envelope for one resource and its ordered evaluations.
type ManifestExplanation struct {
	SchemaVersion       string                    `json:"schemaVersion"`
	Resource            Source                    `json:"resource"`
	ProfileMatch        ProfileMatch              `json:"profileMatch"`
	ContextCompleteness ContextCompleteness       `json:"contextCompleteness"`
	Diagnostics         []Diagnostic              `json:"diagnostics"`
	Evaluations         []ConfigurationEvaluation `json:"evaluations"`
}

// Validate checks envelope vocabulary without changing the wrapped result contract.
func (explanation ManifestExplanation) Validate() error {
	if explanation.SchemaVersion != ExplanationSchemaVersion {
		return &contract.ValidationError{Field: "schemaVersion", Value: explanation.SchemaVersion, Err: contract.ErrInvalidInput}
	}
	if err := explanation.Resource.Validate(); err != nil {
		return fmt.Errorf("resource: %w", err)
	}
	if err := explanation.ProfileMatch.Validate(); err != nil {
		return fmt.Errorf("profileMatch: %w", err)
	}
	if err := explanation.ContextCompleteness.Validate(); err != nil {
		return fmt.Errorf("contextCompleteness: %w", err)
	}
	for i, diagnostic := range explanation.Diagnostics {
		if err := diagnostic.Validate(); err != nil {
			return fmt.Errorf("diagnostics[%d]: %w", i, err)
		}
	}
	for i, evaluation := range explanation.Evaluations {
		if err := evaluation.Configuration.Validate(); err != nil {
			return fmt.Errorf("evaluations[%d].configuration: %w", i, err)
		}
		if evaluation.Result.SchemaVersion != contract.ResultSchemaVersion {
			return &contract.ValidationError{Field: fmt.Sprintf("evaluations[%d].result.schemaVersion", i), Value: evaluation.Result.SchemaVersion, Err: contract.ErrInvalidInput}
		}
		if !contract.IsSupportedCompatibilityProfile(evaluation.Result.CompatibilityProfile) {
			return &contract.ValidationError{Field: fmt.Sprintf("evaluations[%d].result.compatibilityProfile", i), Value: evaluation.Result.CompatibilityProfile.ID, Err: contract.ErrUnsupportedCapability}
		}
		if err := evaluation.Result.Validate(); err != nil {
			return fmt.Errorf("evaluations[%d].result: %w", i, err)
		}
	}
	return nil
}
