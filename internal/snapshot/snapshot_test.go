package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/render"
	"github.com/silbaram/admitrace/internal/resourcecatalog"
	"github.com/silbaram/admitrace/internal/scenario"
)

func TestWriterCreatesByteStableReplayableBundleWithExactPayload(t *testing.T) {
	t.Parallel()

	built := buildSnapshotScenarios(t, `apiVersion: v1
kind: ConfigMap
metadata: {name: example, namespace: team-a}
data:
  explicit-token-like-value: user-supplied-value
  endpoint: https://user-supplied.example
`, manifest.OfflineResolver{}, 2)
	firstTarget := filepath.Join(t.TempDir(), "first")
	secondTarget := filepath.Join(t.TempDir(), "second")
	writer := NewWriter()
	if err := writer.Write(firstTarget, built); err != nil {
		t.Fatalf("Write(first) error = %v", err)
	}
	if err := writer.Write(secondTarget, built); err != nil {
		t.Fatalf("Write(second) error = %v", err)
	}
	assertMode(t, firstTarget, directoryMode)

	for index, item := range built {
		firstPath := filepath.Join(firstTarget, item.SnapshotName)
		secondPath := filepath.Join(secondTarget, item.SnapshotName)
		first, err := os.ReadFile(firstPath)
		if err != nil {
			t.Fatalf("os.ReadFile(first %d) error = %v", index, err)
		}
		second, err := os.ReadFile(secondPath)
		if err != nil {
			t.Fatalf("os.ReadFile(second %d) error = %v", index, err)
		}
		if !bytes.Equal(first, second) {
			t.Errorf("snapshot %d is not byte-stable", index)
		}
		assertMode(t, firstPath, fileMode)
		for _, want := range []string{"explicit-token-like-value", "user-supplied-value", "https://user-supplied.example", "username: alice", "tenant:"} {
			if !bytes.Contains(first, []byte(want)) {
				t.Errorf("snapshot %d lost exact user input %q:\n%s", index, want, first)
			}
		}
		for _, forbidden := range []string{"automatic-bearer-token", "https://cluster.invalid", "context:staging", "client-certificate-data"} {
			if bytes.Contains(first, []byte(forbidden)) {
				t.Errorf("snapshot %d contains automatic connection metadata %q", index, forbidden)
			}
		}

		decoded, err := scenario.Decode(first)
		if err != nil {
			t.Fatalf("scenario.Decode(snapshot %d) error = %v", index, err)
		}
		if !reflect.DeepEqual(decoded.Request.Object, item.Scenario.Request.Object) {
			t.Errorf("snapshot %d resource payload changed\ngot:  %s\nwant: %s", index, decoded.Request.Object, item.Scenario.Request.Object)
		}
		if !reflect.DeepEqual(decoded.Request.UserInfo, item.Scenario.Request.UserInfo) {
			t.Errorf("snapshot %d explicit UserInfo changed: got %#v, want %#v", index, decoded.Request.UserInfo, item.Scenario.Request.UserInfo)
		}
		wantResult := evaluateScenario(t, item.Scenario)
		gotResult := evaluateScenario(t, *decoded)
		wantJSON, err := render.JSON(wantResult)
		if err != nil {
			t.Fatalf("render.JSON(want) error = %v", err)
		}
		gotJSON, err := render.JSON(gotResult)
		if err != nil {
			t.Fatalf("render.JSON(got) error = %v", err)
		}
		if !bytes.Equal(gotJSON, wantJSON) {
			t.Errorf("snapshot %d replay result differs\ngot:\n%s\nwant:\n%s", index, gotJSON, wantJSON)
		}
	}
}

func TestWriterPreservesCustomResourcePayloadWithoutGenericSecretDetection(t *testing.T) {
	t.Parallel()

	resolver, err := manifest.NewVerifiedDiscoveryResolver("verified:test", []resourcecatalog.Resource{{
		Group: "example.io", Version: "v1", Kind: "Widget", Resource: "widgets", Namespaced: true,
	}})
	if err != nil {
		t.Fatalf("NewVerifiedDiscoveryResolver() error = %v", err)
	}
	built := buildSnapshotScenarios(t, `apiVersion: example.io/v1
kind: Widget
metadata: {name: custom, namespace: team-a}
spec:
  password: user-supplied-custom-secret
  nested: {unknownField: keep-me}
`, resolver, 1)
	target := filepath.Join(t.TempDir(), "crd")
	if err := NewWriter().Write(target, built); err != nil {
		t.Fatalf("Write(CRD) error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(target, built[0].SnapshotName))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	for _, want := range []string{"password: user-supplied-custom-secret", "unknownField: keep-me"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("custom resource snapshot lost %q:\n%s", want, data)
		}
	}
	if !strings.Contains(PolicyNotice, "do not detect generic secrets in custom resources") {
		t.Errorf("PolicyNotice = %q, want custom-resource limitation", PolicyNotice)
	}
}

func TestWriterRefusesCoreSecretBeforeCreatingTarget(t *testing.T) {
	t.Parallel()

	built := buildSnapshotScenarios(t, `apiVersion: v1
kind: Secret
metadata: {name: forbidden, namespace: team-a}
stringData: {password: must-not-be-written}
`, manifest.OfflineResolver{}, 1)
	target := filepath.Join(t.TempDir(), "secret-output")
	err := NewWriter().Write(target, built)
	var snapshotError *Error
	if !errors.As(err, &snapshotError) || snapshotError.Kind != ErrorKindPolicy || !errors.Is(err, contract.ErrInvalidInput) {
		t.Fatalf("Write(Secret) error = %v, want policy invalid input", err)
	}
	if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("Secret target exists after refusal: %v", statErr)
	}
	diagnostic := snapshotError.Diagnostic()
	if diagnostic.Code != manifest.DiagnosticCodeSnapshotRefused || !strings.Contains(diagnostic.Message, "core/v1 Secret") {
		t.Errorf("Secret diagnostic = %#v", diagnostic)
	}
}

func TestWriterAcceptsOnlyNonexistentOrEmptyDirectory(t *testing.T) {
	t.Parallel()

	built := buildSnapshotScenarios(t, basicConfigMap, manifest.OfflineResolver{}, 1)
	writer := NewWriter()
	parent := t.TempDir()
	empty := filepath.Join(parent, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatalf("os.Mkdir(empty) error = %v", err)
	}
	if err := writer.Write(empty, built); err != nil {
		t.Fatalf("Write(empty) error = %v", err)
	}
	assertMode(t, empty, directoryMode)

	nonEmpty := filepath.Join(parent, "non-empty")
	if err := os.Mkdir(nonEmpty, 0o700); err != nil {
		t.Fatalf("os.Mkdir(non-empty) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "keep"), []byte("keep"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(keep) error = %v", err)
	}
	err := writer.Write(nonEmpty, built)
	var snapshotError *Error
	if !errors.As(err, &snapshotError) || snapshotError.Kind != ErrorKindDestination {
		t.Errorf("Write(non-empty) error = %v, want destination refusal", err)
	}
	data, readErr := os.ReadFile(filepath.Join(nonEmpty, "keep"))
	if readErr != nil || string(data) != "keep" {
		t.Errorf("non-empty target changed: data %q, error %v", data, readErr)
	}

	fileTarget := filepath.Join(parent, "file")
	if err := os.WriteFile(fileTarget, []byte("keep"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(target) error = %v", err)
	}
	if err := writer.Write(fileTarget, built); err == nil {
		t.Error("Write(file target) error = nil")
	}
}

func TestWriterRejectsFilenameCollisionBeforeFilesystemChanges(t *testing.T) {
	t.Parallel()

	built := buildSnapshotScenarios(t, basicConfigMap, manifest.OfflineResolver{}, 2)
	built[1].SnapshotName = built[0].SnapshotName
	target := filepath.Join(t.TempDir(), "collision")
	err := NewWriter().Write(target, built)
	var snapshotError *Error
	if !errors.As(err, &snapshotError) || snapshotError.Kind != ErrorKindCollision {
		t.Fatalf("Write(collision) error = %v", err)
	}
	if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("collision target exists: %v", statErr)
	}
}

func TestWriterCleansStagingAfterWriteFailureWithoutPartialBundle(t *testing.T) {
	t.Parallel()

	built := buildSnapshotScenarios(t, basicConfigMap, manifest.OfflineResolver{}, 2)
	writer := NewWriter()
	baseWrite := writer.operations.writeFile
	writes := 0
	writer.operations.writeFile = func(path string, data []byte, mode os.FileMode) error {
		writes++
		if writes == 2 {
			return errors.New("injected staged write failure")
		}
		return baseWrite(path, data, mode)
	}
	parent := t.TempDir()
	target := filepath.Join(parent, "failed")
	err := writer.Write(target, built)
	var snapshotError *Error
	if !errors.As(err, &snapshotError) || snapshotError.Kind != ErrorKindStage {
		t.Fatalf("Write(injected failure) error = %v", err)
	}
	if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("partial target exists after staged failure: %v", statErr)
	}
	entries, readErr := os.ReadDir(parent)
	if readErr != nil {
		t.Fatalf("os.ReadDir(parent) error = %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("staging artifacts remain after failure: %#v", entries)
	}
}

func buildSnapshotScenarios(t *testing.T, resourceYAML string, resolver manifest.Resolver, configurationCount int) []manifest.BuiltScenario {
	t.Helper()
	decodedResource, err := manifest.Decode(strings.NewReader(resourceYAML), manifest.SourceKindFile, "resource.yaml")
	if err != nil {
		t.Fatalf("manifest.Decode(resource) error = %v", err)
	}
	configurationYAML := strings.Repeat(snapshotConfiguration+"---\n", configurationCount)
	configurationYAML = strings.TrimSuffix(configurationYAML, "---\n")
	decodedConfigurations, err := manifest.Decode(strings.NewReader(configurationYAML), manifest.SourceKindFile, "webhooks.yaml")
	if err != nil {
		t.Fatalf("manifest.Decode(configurations) error = %v", err)
	}
	configurations := make([]manifest.ConfigurationInput, 0, len(decodedConfigurations.Documents))
	for _, document := range decodedConfigurations.Documents {
		configuration, err := manifest.ConfigurationFromDocument(document)
		if err != nil {
			t.Fatalf("ConfigurationFromDocument() error = %v", err)
		}
		configurations = append(configurations, configuration)
	}
	identity, err := manifest.NewIdentity(manifest.IdentityOptions{User: "alice", Groups: []string{"developers"}, UserExtra: []string{"tenant=blue"}})
	if err != nil {
		t.Fatalf("NewIdentity() error = %v", err)
	}
	built, err := manifest.BuildScenarios(decodedResource.Documents, configurations, resolver, manifest.BuildOptions{Identity: identity})
	if err != nil {
		t.Fatalf("BuildScenarios() error = %v", err)
	}
	return built
}

func evaluateScenario(t *testing.T, input contract.Scenario) contract.EvaluationResult {
	t.Helper()
	snapshot, err := evaluation.SnapshotFromScenario(input)
	if err != nil {
		t.Fatalf("SnapshotFromScenario() error = %v", err)
	}
	return evaluation.NewEvaluator().Evaluate(context.Background(), snapshot)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(%s) error = %v", filepath.Base(path), err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode(%s) = %#o, want %#o", filepath.Base(path), got, want)
	}
}

const basicConfigMap = `apiVersion: v1
kind: ConfigMap
metadata: {name: example, namespace: team-a}
data: {key: value}
`

const snapshotConfiguration = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata: {name: validating}
webhooks:
  - name: validating.example.com
    rules:
      - operations: [CREATE]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
`

func TestSnapshotYAMLContainsValidJSONResourcePayload(t *testing.T) {
	t.Parallel()

	built := buildSnapshotScenarios(t, basicConfigMap, manifest.OfflineResolver{}, 1)
	target := filepath.Join(t.TempDir(), "json-check")
	if err := NewWriter().Write(target, built); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(target, built[0].SnapshotName))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	decoded, err := scenario.Decode(data)
	if err != nil {
		t.Fatalf("scenario.Decode() error = %v", err)
	}
	if !json.Valid(decoded.Request.Object) {
		t.Errorf("snapshot request.object is not JSON: %s", decoded.Request.Object)
	}
}
