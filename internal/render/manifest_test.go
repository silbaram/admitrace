package render_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/render"
)

func TestManifestGoldenViews(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		explanations []manifest.ManifestExplanation
	}{
		{name: "manifest-complete-multi", explanations: []manifest.ManifestExplanation{completeManifestExplanation()}},
		{name: "manifest-incomplete", explanations: []manifest.ManifestExplanation{incompleteManifestExplanation()}},
		{name: "manifest-profile-mismatch", explanations: []manifest.ManifestExplanation{profileMismatchManifestExplanation()}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			jsonOutput, err := render.ManifestJSON(test.explanations)
			if err != nil {
				t.Fatalf("ManifestJSON() error = %v", err)
			}
			textOutput, err := render.ManifestText(test.explanations)
			if err != nil {
				t.Fatalf("ManifestText() error = %v", err)
			}
			assertManifestGolden(t, test.name+".golden.json", jsonOutput)
			assertManifestGolden(t, test.name+".golden.txt", textOutput)
		})
	}
}

func TestManifestRenderersPreserveOrderAndDoNotMutateInput(t *testing.T) {
	t.Parallel()

	first := completeManifestExplanation()
	second := completeManifestExplanation()
	second.Resource = manifest.Source{Kind: manifest.SourceKindFile, Label: "second.yaml", DocumentIndex: 1}
	input := []manifest.ManifestExplanation{first, second}
	before := make([]manifest.Diagnostic, len(input[0].Diagnostics))
	copy(before, input[0].Diagnostics)

	jsonOutput, err := render.ManifestJSON(input)
	if err != nil {
		t.Fatalf("ManifestJSON() error = %v", err)
	}
	textOutput, err := render.ManifestText(input)
	if err != nil {
		t.Fatalf("ManifestText() error = %v", err)
	}
	if !bytes.Contains(jsonOutput, []byte(`"label": "deployment.yaml"`)) || bytes.Index(jsonOutput, []byte("deployment.yaml")) > bytes.Index(jsonOutput, []byte("second.yaml")) {
		t.Errorf("ManifestJSON() did not preserve resource order: %s", jsonOutput)
	}
	if bytes.Count(textOutput, []byte("note: called means routing selected")) != 1 {
		t.Errorf("ManifestText() routing-only note count = %d, want 1", bytes.Count(textOutput, []byte("note: called means routing selected")))
	}
	if !reflect.DeepEqual(input[0].Diagnostics, before) {
		t.Errorf("renderers mutated adapter diagnostics: got %#v, want %#v", input[0].Diagnostics, before)
	}
}

func TestManifestRenderersRejectInvalidOrEmptyEnvelope(t *testing.T) {
	t.Parallel()

	for _, renderer := range []struct {
		name   string
		render func([]manifest.ManifestExplanation) ([]byte, error)
	}{
		{name: "json", render: render.ManifestJSON},
		{name: "text", render: render.ManifestText},
	} {
		t.Run(renderer.name, func(t *testing.T) {
			if output, err := renderer.render(nil); output != nil || !errors.Is(err, contract.ErrInvalidInput) {
				t.Errorf("render(nil) = (%q, %v), want nil/invalid input", output, err)
			}
			invalid := completeManifestExplanation()
			invalid.ContextCompleteness.Configuration.Status = "unknown"
			if output, err := renderer.render([]manifest.ManifestExplanation{invalid}); output != nil || !errors.Is(err, contract.ErrInvalidInput) {
				t.Errorf("render(invalid) = (%q, %v), want nil/invalid input", output, err)
			}
		})
	}
}

func completeManifestExplanation() manifest.ManifestExplanation {
	first := determinateResult()
	first.ScenarioID = "manifest-r0001-c0001"
	second := determinateResult()
	second.ScenarioID = "manifest-r0001-c0002"
	return manifest.ManifestExplanation{
		SchemaVersion: manifest.ExplanationSchemaVersion,
		Resource:      manifest.Source{Kind: manifest.SourceKindFile, Label: "deployment.yaml", DocumentIndex: 2},
		ProfileMatch: manifest.ProfileMatch{
			Status:  manifest.ProfileMatchDeclared,
			Profile: contract.Kubernetes136DefaultProfile(),
		},
		ContextCompleteness: manifest.ContextCompleteness{
			Configuration: manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: "webhooks.yaml"},
			Discovery:     manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: contract.Kubernetes136DefaultProfileID},
			Namespace:     manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
			Identity:      manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: "explicit-admission-user"},
			Equivalence:   manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
			Authorization: manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
		},
		Diagnostics: []manifest.Diagnostic{},
		Evaluations: []manifest.ConfigurationEvaluation{
			{Configuration: manifest.Source{Kind: manifest.SourceKindFile, Label: "webhooks.yaml", DocumentIndex: 1}, Result: first},
			{Configuration: manifest.Source{Kind: manifest.SourceKindFile, Label: "webhooks.yaml", DocumentIndex: 2}, Result: second},
		},
	}
}

func incompleteManifestExplanation() manifest.ManifestExplanation {
	result := indeterminateResult()
	result.ScenarioID = "manifest-r0001-c0001"
	return manifest.ManifestExplanation{
		SchemaVersion: manifest.ExplanationSchemaVersion,
		Resource:      manifest.Source{Kind: manifest.SourceKindDirectoryEntry, Label: "20-pod.yaml", DocumentIndex: 1},
		ProfileMatch: manifest.ProfileMatch{
			Status:                    manifest.ProfileMatchVerified,
			Profile:                   contract.Kubernetes136DefaultProfile(),
			ObservedKubernetesVersion: "1.36.2",
		},
		ContextCompleteness: manifest.ContextCompleteness{
			Configuration: manifest.ContextStatus{Status: manifest.CompletenessForbidden},
			Discovery:     manifest.ContextStatus{Status: manifest.CompletenessHydrated, SourceLabel: "context:staging"},
			Namespace:     manifest.ContextStatus{Status: manifest.CompletenessMissing},
			Identity:      manifest.ContextStatus{Status: manifest.CompletenessMissing},
			Equivalence:   manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
			Authorization: manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
		},
		Diagnostics: []manifest.Diagnostic{
			{Code: manifest.DiagnosticCodeIdentityContextMissing, Severity: contract.DiagnosticSeverityWarning, Message: "explicit admission identity is required", SourceLabel: "20-pod.yaml", DocumentIndex: 1},
			{Code: manifest.DiagnosticCodeIncomplete, Severity: contract.DiagnosticSeverityWarning, Message: "configuration LIST was forbidden; provide --webhook-config", SourceLabel: "20-pod.yaml", DocumentIndex: 1},
		},
		Evaluations: []manifest.ConfigurationEvaluation{{
			Configuration: manifest.Source{Kind: manifest.SourceKindCluster, Label: "context:staging/ValidatingWebhookConfiguration/partial"},
			Result:        result,
		}},
	}
}

func profileMismatchManifestExplanation() manifest.ManifestExplanation {
	return manifest.ManifestExplanation{
		SchemaVersion: manifest.ExplanationSchemaVersion,
		Resource:      manifest.Source{Kind: manifest.SourceKindStdin, Label: "stdin", DocumentIndex: 1},
		ProfileMatch: manifest.ProfileMatch{
			Status:                    manifest.ProfileMatchMismatch,
			Profile:                   contract.Kubernetes136DefaultProfile(),
			ObservedKubernetesVersion: "1.36.3",
		},
		ContextCompleteness: manifest.ContextCompleteness{
			Configuration: manifest.ContextStatus{Status: manifest.CompletenessMissing},
			Discovery:     manifest.ContextStatus{Status: manifest.CompletenessUnsupported},
			Namespace:     manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
			Identity:      manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
			Equivalence:   manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
			Authorization: manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
		},
		Diagnostics: []manifest.Diagnostic{{
			Code:          manifest.DiagnosticCodeProfileMismatch,
			Severity:      contract.DiagnosticSeverityWarning,
			Message:       "connected Kubernetes version does not match exact 1.36.2",
			SourceLabel:   "stdin",
			DocumentIndex: 1,
		}},
		Evaluations: []manifest.ConfigurationEvaluation{},
	}
}

func assertManifestGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", name, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s drifted\ngot:\n%s\nwant:\n%s", name, got, want)
	}
}
