package manifest_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/scenario"
)

func TestDecodeResourceDocumentsStrictlyAndInOrder(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`apiVersion: v1
kind: ConfigMap
metadata:
  name: first
data:
  arbitrary: allowed
`,
		`apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: validating
webhooks: []
`,
		`{"apiVersion":"admissionregistration.k8s.io/v1","kind":"MutatingWebhookConfiguration","metadata":{"name":"mutating"},"webhooks":[]}
`,
		`apiVersion: v1
kind: Namespace
metadata:
  name: test
`,
	}, "---\n")

	decoded, err := manifest.Decode(strings.NewReader(input), manifest.SourceKindStdin, "/private/credential/input.yaml")
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Mode != manifest.InputModeResource || decoded.Scenario != nil {
		t.Fatalf("Decode() mode/scenario = (%q, %v), want resource/nil", decoded.Mode, decoded.Scenario)
	}
	wantClasses := []manifest.DocumentClass{
		manifest.DocumentClassResource,
		manifest.DocumentClassValidatingConfiguration,
		manifest.DocumentClassMutatingConfiguration,
		manifest.DocumentClassNamespace,
	}
	gotClasses := make([]manifest.DocumentClass, 0, len(decoded.Documents))
	for index, document := range decoded.Documents {
		gotClasses = append(gotClasses, document.Class)
		if document.Source.Label != "stdin" || document.Source.DocumentIndex != index+1 {
			t.Errorf("document %d source = %#v, want stdin document %d", index, document.Source, index+1)
		}
		if strings.Contains(document.Source.Label, "/private/") || strings.Contains(document.Source.Label, "credential") {
			t.Errorf("source label %q contains sensitive path material", document.Source.Label)
		}
	}
	if !slices.Equal(gotClasses, wantClasses) {
		t.Errorf("classes = %v, want %v", gotClasses, wantClasses)
	}
	if decoded.Documents[1].ValidatingConfiguration == nil || decoded.Documents[2].MutatingConfiguration == nil || decoded.Documents[3].Namespace == nil {
		t.Error("typed configuration or Namespace projection is missing")
	}
}

func TestDecodeRejectsDocumentLocalViolations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantIndex int
	}{
		{
			name: "empty first document",
			input: `---
---
apiVersion: v1
kind: ConfigMap
`,
			wantIndex: 1,
		},
		{
			name: "empty second document",
			input: `apiVersion: v1
kind: ConfigMap
---
`,
			wantIndex: 2,
		},
		{
			name: "invalid second document",
			input: `apiVersion: v1
kind: ConfigMap
---
apiVersion: [
`,
			wantIndex: 2,
		},
		{
			name: "duplicate YAML key in second document",
			input: `apiVersion: v1
kind: ConfigMap
---
apiVersion: v1
kind: ConfigMap
kind: Secret
`,
			wantIndex: 2,
		},
		{
			name:      "duplicate JSON key",
			input:     `{"apiVersion":"v1","kind":"ConfigMap","kind":"Secret"}`,
			wantIndex: 1,
		},
		{
			name:      "typed unknown field",
			input:     `{"apiVersion":"admissionregistration.k8s.io/v1","kind":"ValidatingWebhookConfiguration","unexpected":true,"webhooks":[]}`,
			wantIndex: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := manifest.Decode(strings.NewReader(test.input), manifest.SourceKindFile, "/tmp/secret/input.yaml")
			requireDocumentError(t, err, "input.yaml", test.wantIndex)
		})
	}
}

func TestDecodeAppliesLimitsPerDocument(t *testing.T) {
	t.Parallel()

	input := "apiVersion: v1\nkind: ConfigMap\n---\n" + strings.Repeat("x", scenario.MaximumDocumentBytes+1)
	_, err := manifest.Decode(strings.NewReader(input), manifest.SourceKindFile, "large.yaml")
	requireDocumentError(t, err, "large.yaml", 2)
	if !errors.Is(err, contract.ErrResourceLimit) {
		t.Fatalf("Decode() error = %v, want resource limit", err)
	}
}

func TestDecodeModeBoundary(t *testing.T) {
	t.Parallel()

	decoded, err := manifest.Decode(strings.NewReader(validScenario), manifest.SourceKindFile, "scenario.yaml")
	if err != nil {
		t.Fatalf("Decode(single Scenario) error = %v", err)
	}
	if decoded.Mode != manifest.InputModeLegacyScenario || decoded.Scenario == nil || len(decoded.Documents) != 0 {
		t.Fatalf("Decode(single Scenario) = %#v, want legacy Scenario", decoded)
	}

	mixed := "apiVersion: v1\nkind: ConfigMap\n---\n" + validScenario
	_, err = manifest.Decode(strings.NewReader(mixed), manifest.SourceKindStdin, "-")
	requireDocumentError(t, err, "stdin", 2)
}

func TestDecodeDirectoryIsDeterministicAndResourceOnly(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeTestFile(t, directory, "20-b.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: b}\n")
	writeTestFile(t, directory, "10-a.yml", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: a}\n---\napiVersion: v1\nkind: Namespace\nmetadata: {name: a}\n")
	writeTestFile(t, directory, "30-c.json", `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"c"}}`)
	writeTestFile(t, directory, "00-ignore.txt", "apiVersion: v1\nkind: Secret\n")
	if err := os.Symlink(filepath.Join(directory, "20-b.yaml"), filepath.Join(directory, "05-link.yaml")); err != nil {
		t.Fatalf("os.Symlink(file) error = %v", err)
	}

	decoded, err := manifest.DecodeDirectory(directory)
	if err != nil {
		t.Fatalf("DecodeDirectory() error = %v", err)
	}
	wantSources := []string{"10-a.yml#1", "10-a.yml#2", "20-b.yaml#1", "30-c.json#1"}
	gotSources := make([]string, 0, len(decoded.Documents))
	for _, document := range decoded.Documents {
		gotSources = append(gotSources, document.Source.Label+"#"+strconv.Itoa(document.Source.DocumentIndex))
	}
	if !slices.Equal(gotSources, wantSources) {
		t.Errorf("sources = %v, want %v", gotSources, wantSources)
	}

	writeTestFile(t, directory, "25-scenario.yaml", validScenario)
	_, err = manifest.DecodeDirectory(directory)
	requireDocumentError(t, err, "25-scenario.yaml", 1)
}

func TestDecodeDirectoryRejectsSymlinkAndEmptyDirectory(t *testing.T) {
	t.Parallel()

	empty := t.TempDir()
	_, err := manifest.DecodeDirectory(empty)
	requireDocumentError(t, err, filepath.Base(empty), 0)

	parent := t.TempDir()
	link := filepath.Join(parent, "linked")
	if err := os.Symlink(empty, link); err != nil {
		t.Fatalf("os.Symlink(directory) error = %v", err)
	}
	_, err = manifest.DecodeDirectory(link)
	requireDocumentError(t, err, "linked", 0)
}

func requireDocumentError(t *testing.T, err error, wantLabel string, wantIndex int) {
	t.Helper()
	if !errors.Is(err, contract.ErrInvalidInput) {
		t.Fatalf("error = %v, want invalid input", err)
	}
	var documentError *manifest.DocumentError
	if !errors.As(err, &documentError) {
		t.Fatalf("error type = %T, want *manifest.DocumentError", err)
	}
	if documentError.Source.Label != wantLabel || documentError.Source.DocumentIndex != wantIndex {
		t.Errorf("source = %#v, want label/index (%q, %d)", documentError.Source, wantLabel, wantIndex)
	}
}

func writeTestFile(t *testing.T, directory, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", name, err)
	}
}

const validScenario = `apiVersion: admitrace.io/v1alpha1
kind: Scenario
metadata:
  name: legacy
compatibilityProfile:
  id: kubernetes-1.36.2-defaults
  kubernetesVersion: 1.36.2
  featureGatePolicy: kubernetes-defaults
configuration:
  validatingWebhookConfiguration:
    apiVersion: admissionregistration.k8s.io/v1
    kind: ValidatingWebhookConfiguration
    webhooks:
      - name: example.policy.test
        rules:
          - operations: [CREATE]
            apiGroups: [""]
            apiVersions: [v1]
            resources: [pods]
request:
  kind:
    version: v1
    kind: Pod
  resource:
    version: v1
    resource: pods
  operation: CREATE
  scope: Namespaced
  userInfo:
    username: alice
`
