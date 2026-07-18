package cli

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

	"github.com/silbaram/admitrace/internal/adapter"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/hydration"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/resourcecatalog"
)

func TestOfflineManifestExplanationIsCanonicalAndNetworkFree(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	resourcePath := filepath.Join(directory, "resources.yaml")
	configurationPath := filepath.Join(directory, "configurations.yaml")
	writeCLIFile(t, resourcePath, `apiVersion: v1
kind: Pod
metadata: {name: first, namespace: team-a}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: second, namespace: team-a}
spec:
  selector: {matchLabels: {app: second}}
  template:
    metadata: {labels: {app: second}}
    spec: {containers: [{name: app, image: example.invalid/app:v1}]}
`)
	writeCLIFile(t, configurationPath, validatingCLIConfiguration("first.example.com")+"---\n"+mutatingCLIConfiguration("second.example.com"))

	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			calls := 0
			args := []string{"explain", "-f", resourcePath, "--webhook-config", configurationPath, "-o", format}
			firstCode, first, firstStderr := executeManifestExplain(t, args, "", &calls)
			secondCode, second, secondStderr := executeManifestExplain(t, args, "", &calls)
			if firstCode != ExitSuccess || secondCode != ExitSuccess || firstStderr != "" || secondStderr != "" {
				t.Fatalf("Execute(%s) = codes (%d, %d), stderr (%q, %q)", format, firstCode, secondCode, firstStderr, secondStderr)
			}
			if first != second {
				t.Errorf("canonical %s output changed between identical offline runs", format)
			}
			if calls != 0 {
				t.Errorf("offline hydration/client construction calls = %d, want zero", calls)
			}
			if !strings.HasSuffix(first, "\n") {
				t.Errorf("canonical %s output has no final newline", format)
			}
			if format == "json" {
				if !json.Valid([]byte(first)) {
					t.Errorf("canonical manifest output is not valid JSON")
				}
				assertOrderedOfflineProduct(t, first)
				return
			}
			for _, want := range []string{
				`resourceSource: "resources.yaml" document 1`,
				`resourceSource: "resources.yaml" document 2`,
				`source: "configurations.yaml" document 1`,
				`source: "configurations.yaml" document 2`,
				"called means routing selected the webhook; no HTTP request was sent",
			} {
				if !strings.Contains(first, want) {
					t.Errorf("canonical text output missing %q:\n%s", want, first)
				}
			}
		})
	}
}

func TestOfflineManifestInputFailuresAreSourceIndexedAndFailClosed(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	configurationPath := filepath.Join(directory, "configuration.yaml")
	writeCLIFile(t, configurationPath, validatingCLIConfiguration("policy.example.com"))

	tests := []struct {
		name     string
		input    string
		wantCode ExitCode
		want     []string
	}{
		{
			name:     "invalid second document",
			input:    "apiVersion: v1\nkind: Pod\nmetadata: {name: first}\n---\nkind: Pod\nmetadata: {name: second}\n",
			wantCode: ExitInvalidInput,
			want:     []string{"stdin document 2", "apiVersion is required"},
		},
		{
			name:     "empty second document",
			input:    "apiVersion: v1\nkind: Pod\nmetadata: {name: first}\n---\n---\napiVersion: v1\nkind: Pod\nmetadata: {name: third}\n",
			wantCode: ExitInvalidInput,
			want:     []string{"stdin document 2", "empty document"},
		},
		{
			name:     "mixed Scenario and resource",
			input:    internalExplainScenario() + "---\napiVersion: v1\nkind: Pod\nmetadata: {name: mixed}\n",
			wantCode: ExitInvalidInput,
			want:     []string{"stdin document 1", "Scenario cannot be mixed"},
		},
		{
			name:     "unknown offline CRD",
			input:    "apiVersion: example.com/v1\nkind: Widget\nmetadata: {name: unknown}\n",
			wantCode: ExitIncompleteEvaluation,
			want:     []string{"stdin document 1", "not present in the embedded Kubernetes 1.36.2 catalog", "use verified discovery for CRDs"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			code, stdout, stderr := executeManifestExplain(t, []string{
				"explain", "-f", "-", "--webhook-config", configurationPath, "-o", "json",
			}, test.input, &calls)
			if code != test.wantCode || stdout != "" {
				t.Fatalf("Execute() = code %d, stdout %q; want code %d and no partial output", code, stdout, test.wantCode)
			}
			for _, want := range test.want {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr missing %q: %s", want, stderr)
				}
			}
			if calls != 0 {
				t.Errorf("hydration/client construction after offline failure = %d, want zero", calls)
			}
		})
	}
}

func TestOfflineManifestMissingContextsRemainSourceIndexedAndFailClosed(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	configurationPath := filepath.Join(directory, "contexts.yaml")
	writeCLIFile(t, configurationPath, strings.Join([]string{
		validatingCLIConfigurationWithSelector(),
		validatingCLIIdentityConfiguration(),
		validatingCLIAuthorizationConfiguration(),
		validatingCLIEquivalenceConfiguration(),
	}, "---\n"))
	resource := "apiVersion: v1\nkind: Pod\nmetadata: {name: example, namespace: team-a}\n"

	calls := 0
	code, stdout, stderr := executeManifestExplain(t, []string{
		"explain", "--resource", "-", "--webhook-config", configurationPath, "-o", "json",
	}, resource, &calls)
	if code != ExitIncompleteEvaluation || stderr != "" {
		t.Fatalf("Execute(missing contexts) = code %d, stderr %q", code, stderr)
	}
	if calls != 0 {
		t.Errorf("offline hydration/client construction calls = %d, want zero", calls)
	}

	var explanation manifest.ManifestExplanation
	if err := json.Unmarshal([]byte(stdout), &explanation); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output = %s", err, stdout)
	}
	if explanation.Resource.Label != "stdin" || explanation.Resource.DocumentIndex != 1 {
		t.Errorf("resource source = %#v, want stdin document 1", explanation.Resource)
	}
	if len(explanation.Evaluations) != 4 {
		t.Fatalf("configuration evaluations = %d, want 4", len(explanation.Evaluations))
	}
	wantReasons := []contract.ReasonCode{
		contract.ReasonCodeNamespaceContextMissing,
		contract.ReasonCodeIdentityContextMissing,
		contract.ReasonCodeAuthorizationContextMissing,
		contract.ReasonCodeEquivalenceContextMissing,
	}
	for index, want := range wantReasons {
		got := terminalReason(explanation.Evaluations[index].Result)
		if got != want {
			t.Errorf("configuration document %d terminal reason = %q, want %q", index+1, got, want)
		}
		if explanation.Evaluations[index].Configuration.Label != "contexts.yaml" || explanation.Evaluations[index].Configuration.DocumentIndex != index+1 {
			t.Errorf("configuration source %d = %#v", index+1, explanation.Evaluations[index].Configuration)
		}
	}
	for name, status := range map[string]manifest.ContextStatus{
		"namespace":     explanation.ContextCompleteness.Namespace,
		"identity":      explanation.ContextCompleteness.Identity,
		"equivalence":   explanation.ContextCompleteness.Equivalence,
		"authorization": explanation.ContextCompleteness.Authorization,
	} {
		if status.Status != manifest.CompletenessMissing {
			t.Errorf("%s completeness = %#v, want missing", name, status)
		}
	}
	for _, diagnostic := range explanation.Diagnostics {
		if diagnostic.SourceLabel != "" && (diagnostic.SourceLabel != "stdin" || diagnostic.DocumentIndex != 1) {
			t.Errorf("adapter diagnostic is not resource-indexed: %#v", diagnostic)
		}
	}
}

func TestOfflineSnapshotCLIReplayPreservesExactEvaluationAndPayload(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	resourcePath := filepath.Join(directory, "resource.yaml")
	configurationPath := filepath.Join(directory, "configuration.yaml")
	target := filepath.Join(directory, "snapshots")
	writeCLIFile(t, resourcePath, `apiVersion: example.com/v1
kind: Widget
metadata: {name: example}
spec:
  password: preserve-this-explicit-value
  unknown: {nested: [one, two]}
`)
	writeCLIFile(t, configurationPath, validatingCLIConfiguration("policy.example.com"))

	discovery := &cliFakeReader{
		discovery: hydration.DiscoveryResult{Status: hydration.ReadStatusSuccess, Resources: []resourcecatalog.Resource{
			{Group: "example.com", Version: "v1", Kind: "Widget", Resource: "widgets", Namespaced: false},
		}},
	}
	prepareCalls := 0
	var originalStdout bytes.Buffer
	var originalStderr bytes.Buffer
	dependencies := defaultCommandDependencies()
	dependencies.prepareHydration = func(context.Context, hydration.Options) (*adapter.Hydration, error) {
		prepareCalls++
		return &adapter.Hydration{
			Reader:      discovery,
			SourceLabel: "context:integration",
			ProfileMatch: manifest.ProfileMatch{
				Status:                    manifest.ProfileMatchVerified,
				Profile:                   contract.Kubernetes136DefaultProfile(),
				ObservedKubernetesVersion: "1.36.2",
			},
		}, nil
	}
	originalCode := executeWithDependencies([]string{
		"explain", "--resource", resourcePath, "--webhook-config", configurationPath,
		"--context", "integration", "--user", "alice", "--user-extra", "tenant=blue",
		"--snapshot-out", target, "-o", "json",
	}, strings.NewReader(""), &originalStdout, &originalStderr, BuildMetadata{}, dependencies)
	if originalCode != ExitSuccess || originalStderr.String() != "" {
		t.Fatalf("Execute(snapshot source) = code %d, stderr %q", originalCode, originalStderr.String())
	}
	if prepareCalls != 1 {
		t.Fatalf("explicit hydration preparations = %d, want 1", prepareCalls)
	}

	snapshotPath := filepath.Join(target, "0001-0001.yaml")
	assertSnapshotModes(t, target, snapshotPath)
	snapshotBytes, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("os.ReadFile(snapshot) error = %v", err)
	}
	for _, want := range []string{"preserve-this-explicit-value", "unknown", "alice", "tenant", "blue"} {
		if !bytes.Contains(snapshotBytes, []byte(want)) {
			t.Errorf("snapshot payload missing %q:\n%s", want, snapshotBytes)
		}
	}

	var replayStdout bytes.Buffer
	var replayStderr bytes.Buffer
	replayCalls := 0
	replayCode := executeWithDependencies(
		[]string{"explain", "-f", snapshotPath, "-o", "json"},
		strings.NewReader(""), &replayStdout, &replayStderr, BuildMetadata{},
		commandDependencies{prepareHydration: func(context.Context, hydration.Options) (*adapter.Hydration, error) {
			replayCalls++
			return nil, errors.New("snapshot replay must be offline")
		}},
	)
	if replayCode != ExitSuccess || replayStderr.String() != "" || replayCalls != 0 {
		t.Fatalf("Execute(snapshot replay) = code %d, stderr %q, hydration calls %d", replayCode, replayStderr.String(), replayCalls)
	}

	var original manifest.ManifestExplanation
	if err := json.Unmarshal(originalStdout.Bytes(), &original); err != nil {
		t.Fatalf("decode original explanation: %v", err)
	}
	var replay contract.EvaluationResult
	if err := json.Unmarshal(replayStdout.Bytes(), &replay); err != nil {
		t.Fatalf("decode replay explanation: %v", err)
	}
	if len(original.Evaluations) != 1 || !reflect.DeepEqual(replay, original.Evaluations[0].Result) {
		t.Errorf("snapshot replay evaluation drifted\noriginal: %#v\nreplay: %#v", original.Evaluations, replay)
	}
}

func assertOrderedOfflineProduct(t *testing.T, output string) {
	t.Helper()
	var explanations []manifest.ManifestExplanation
	if err := json.Unmarshal([]byte(output), &explanations); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(explanations) != 2 {
		t.Fatalf("resource explanations = %d, want 2", len(explanations))
	}
	wantIDs := [][]string{
		{"manifest-r0001-c0001", "manifest-r0001-c0002"},
		{"manifest-r0002-c0001", "manifest-r0002-c0002"},
	}
	for resourceIndex := range explanations {
		explanation := explanations[resourceIndex]
		if explanation.Resource.Label != "resources.yaml" || explanation.Resource.DocumentIndex != resourceIndex+1 {
			t.Errorf("resource %d source = %#v", resourceIndex+1, explanation.Resource)
		}
		if explanation.ProfileMatch.Status != manifest.ProfileMatchDeclared || len(explanation.Evaluations) != 2 {
			t.Errorf("resource %d explanation shape = %#v", resourceIndex+1, explanation)
			continue
		}
		for configurationIndex := range explanation.Evaluations {
			evaluation := explanation.Evaluations[configurationIndex]
			if evaluation.Configuration.Label != "configurations.yaml" || evaluation.Configuration.DocumentIndex != configurationIndex+1 {
				t.Errorf("resource %d configuration %d source = %#v", resourceIndex+1, configurationIndex+1, evaluation.Configuration)
			}
			if evaluation.Result.ScenarioID != wantIDs[resourceIndex][configurationIndex] {
				t.Errorf("scenario ID = %q, want %q", evaluation.Result.ScenarioID, wantIDs[resourceIndex][configurationIndex])
			}
		}
	}
}

func terminalReason(result contract.EvaluationResult) contract.ReasonCode {
	if len(result.Webhooks) == 0 {
		return ""
	}
	trace := result.Webhooks[0].Trace
	for index := len(trace) - 1; index >= 0; index-- {
		if trace[index].Terminal {
			return trace[index].ReasonCode
		}
	}
	return ""
}

func assertSnapshotModes(t *testing.T, directory, file string) {
	t.Helper()
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		t.Fatalf("os.Stat(snapshot directory) error = %v", err)
	}
	fileInfo, err := os.Stat(file)
	if err != nil {
		t.Fatalf("os.Stat(snapshot file) error = %v", err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("snapshot directory mode = %04o, want 0700", got)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("snapshot file mode = %04o, want 0600", got)
	}
}

func validatingCLIAuthorizationConfiguration() string {
	return `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata: {name: authorization}
webhooks:
  - name: authorization.example.com
    matchConditions:
      - name: authorization
        expression: authorizer.group('').resource('pods').namespace('team-a').name('example').check('get').allowed()
    rules:
      - operations: [CREATE]
        apiGroups: [""]
        apiVersions: [v1]
        resources: [pods]
`
}

func validatingCLIEquivalenceConfiguration() string {
	return `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata: {name: equivalence}
webhooks:
  - name: equivalence.example.com
    matchPolicy: Equivalent
    rules:
      - operations: [CREATE]
        apiGroups: [batch]
        apiVersions: [v1]
        resources: [jobs]
`
}
