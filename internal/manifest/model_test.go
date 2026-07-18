package manifest_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/manifest"
)

func TestCompletenessVocabulary(t *testing.T) {
	t.Parallel()

	valid := []manifest.CompletenessStatus{
		manifest.CompletenessProvided,
		manifest.CompletenessHydrated,
		manifest.CompletenessNotRequired,
		manifest.CompletenessMissing,
		manifest.CompletenessForbidden,
		manifest.CompletenessUnsupported,
	}
	for _, status := range valid {
		if !status.IsValid() {
			t.Errorf("CompletenessStatus(%q).IsValid() = false, want true", status)
		}
	}
	if manifest.CompletenessStatus("unknown").IsValid() {
		t.Error("CompletenessStatus(unknown).IsValid() = true, want false")
	}
}

func TestAdapterDiagnosticCodesRemainSeparateFromEvaluationErrors(t *testing.T) {
	t.Parallel()

	codes := []manifest.DiagnosticCode{
		manifest.DiagnosticCodeIncomplete,
		manifest.DiagnosticCodeProfileMismatch,
		manifest.DiagnosticCodeIdentityContextMissing,
		manifest.DiagnosticCodeSnapshotRefused,
		manifest.DiagnosticCodeUnsupportedOperation,
	}
	for _, code := range codes {
		if !code.IsValid() {
			t.Errorf("DiagnosticCode(%q).IsValid() = false, want true", code)
		}
		if string(code) == string(contract.ReasonCodeInvalidInput) || string(code) == string(contract.ReasonCodeInternalError) {
			t.Errorf("adapter diagnostic %q overlaps an evaluation error reason", code)
		}
	}
}

func TestManifestExplanationValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*manifest.ManifestExplanation)
		wantError error
	}{
		{name: "valid"},
		{
			name: "zero document index",
			mutate: func(explanation *manifest.ManifestExplanation) {
				explanation.Resource.DocumentIndex = 0
			},
			wantError: contract.ErrInvalidInput,
		},
		{
			name: "unknown completeness status",
			mutate: func(explanation *manifest.ManifestExplanation) {
				explanation.ContextCompleteness.Identity.Status = "unknown"
			},
			wantError: contract.ErrInvalidEnumValue,
		},
		{
			name: "hydrated context without source",
			mutate: func(explanation *manifest.ManifestExplanation) {
				explanation.ContextCompleteness.Namespace = manifest.ContextStatus{Status: manifest.CompletenessHydrated}
			},
			wantError: contract.ErrInvalidInput,
		},
		{
			name: "unknown adapter diagnostic",
			mutate: func(explanation *manifest.ManifestExplanation) {
				explanation.Diagnostics[0].Code = "UNKNOWN"
			},
			wantError: contract.ErrUnregisteredReasonCode,
		},
		{
			name: "changed result schema",
			mutate: func(explanation *manifest.ManifestExplanation) {
				explanation.Evaluations[0].Result.SchemaVersion = "admitrace.result/v2"
			},
			wantError: contract.ErrInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			explanation := testExplanation()
			if test.mutate != nil {
				test.mutate(&explanation)
			}
			err := explanation.Validate()
			if !errors.Is(err, test.wantError) {
				t.Errorf("Validate() error = %v, want errors.Is(..., %v)", err, test.wantError)
			}
		})
	}
}

func TestManifestExplanationGoldenAndResultCompatibility(t *testing.T) {
	t.Parallel()

	explanation := testExplanation()
	if err := explanation.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	resultBefore, err := json.Marshal(explanation.Evaluations[0].Result)
	if err != nil {
		t.Fatalf("json.Marshal(result) error = %v", err)
	}
	encoded, err := json.MarshalIndent(explanation, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent(explanation) error = %v", err)
	}
	encoded = append(encoded, '\n')
	resultAfter, err := json.Marshal(explanation.Evaluations[0].Result)
	if err != nil {
		t.Fatalf("json.Marshal(wrapped result) error = %v", err)
	}
	if !bytes.Equal(resultBefore, resultAfter) {
		t.Errorf("wrapping changed EvaluationResult JSON: got %s, want %s", resultAfter, resultBefore)
	}

	want, err := os.ReadFile(filepath.Join("testdata", "explanation.golden.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(golden) error = %v", err)
	}
	if !bytes.Equal(encoded, want) {
		t.Errorf("manifest explanation drift detected\ngot:\n%s\nwant:\n%s", encoded, want)
	}

	var decoded manifest.ManifestExplanation
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(explanation) error = %v", err)
	}
	if !reflect.DeepEqual(decoded, explanation) {
		t.Errorf("JSON round trip mismatch: got %#v, want %#v", decoded, explanation)
	}
}

func testExplanation() manifest.ManifestExplanation {
	result := contract.EvaluationResult{
		SchemaVersion:        contract.ResultSchemaVersion,
		ScenarioID:           "deployment-2-validating-config",
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		EvaluationPhase:      contract.EvaluationPhaseSnapshotRouting,
		ConfigurationKind:    contract.ConfigurationKindValidating,
		Webhooks:             []contract.WebhookEvaluation{},
		Diagnostics:          []contract.Diagnostic{},
	}
	return manifest.ManifestExplanation{
		SchemaVersion: manifest.ExplanationSchemaVersion,
		Resource: manifest.Source{
			Kind:          manifest.SourceKindFile,
			Label:         "deployment.yaml",
			DocumentIndex: 2,
		},
		ProfileMatch: manifest.ProfileMatch{
			Status:  manifest.ProfileMatchDeclared,
			Profile: contract.Kubernetes136DefaultProfile(),
		},
		ContextCompleteness: manifest.ContextCompleteness{
			Configuration: manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: "webhooks.yaml"},
			Discovery:     manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: contract.Kubernetes136DefaultProfileID},
			Namespace:     manifest.ContextStatus{Status: manifest.CompletenessMissing},
			Identity:      manifest.ContextStatus{Status: manifest.CompletenessMissing},
			Equivalence:   manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
			Authorization: manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
		},
		Diagnostics: []manifest.Diagnostic{
			{
				Code:          manifest.DiagnosticCodeIncomplete,
				Severity:      contract.DiagnosticSeverityWarning,
				Message:       "namespace and identity context are unavailable",
				SourceLabel:   "deployment.yaml",
				DocumentIndex: 2,
			},
		},
		Evaluations: []manifest.ConfigurationEvaluation{
			{
				Configuration: manifest.Source{Kind: manifest.SourceKindFile, Label: "webhooks.yaml", DocumentIndex: 1},
				Result:        result,
			},
		},
	}
}
