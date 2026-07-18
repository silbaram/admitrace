package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/adapter"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/hydration"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/resourcecatalog"
	"github.com/silbaram/admitrace/internal/scenario"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUniversalExplainKeepsLegacyOutputAndSkipsHydrationFactory(t *testing.T) {
	t.Parallel()

	input := internalExplainScenario()
	legacyData, err := readScenario(strings.NewReader(input), "-")
	if err != nil {
		t.Fatalf("readScenario() error = %v", err)
	}
	legacyResult, err := explainLegacyBytes(t.Context(), legacyData)
	if err != nil {
		t.Fatalf("explainLegacyBytes() error = %v", err)
	}
	want, err := renderExplanation(legacyResult, outputJSON)
	if err != nil {
		t.Fatalf("renderExplanation() error = %v", err)
	}

	calls := 0
	code, stdout, stderr := executeManifestExplain(t, []string{"explain", "-f", "-", "-o", "json"}, input, &calls)
	if code != ExitSuccess || stderr != "" {
		t.Fatalf("Execute(legacy) = code %d, stderr %q", code, stderr)
	}
	if stdout != string(want) {
		t.Errorf("universal legacy output drifted\ngot:\n%s\nwant:\n%s", stdout, want)
	}
	if calls != 0 {
		t.Errorf("offline legacy hydration factory calls = %d, want zero", calls)
	}
}

func TestUniversalExplainRunsOrderedResourceConfigurationProductOffline(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	resourcePath := filepath.Join(directory, "resources.yaml")
	configurationPath := filepath.Join(directory, "configurations.yaml")
	writeCLIFile(t, resourcePath, `apiVersion: v1
kind: Pod
metadata: {name: first, namespace: team-a}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: second, namespace: team-a}
`)
	writeCLIFile(t, configurationPath, validatingCLIConfiguration("first.example.com")+"---\n"+mutatingCLIConfiguration("second.example.com"))

	calls := 0
	code, stdout, stderr := executeManifestExplain(t, []string{
		"explain", "--resource", resourcePath, "--webhook-config", configurationPath, "-o", "json",
	}, "", &calls)
	if code != ExitSuccess || stderr != "" {
		t.Fatalf("Execute(resource product) = code %d, stderr %q", code, stderr)
	}
	var explanations []manifest.ManifestExplanation
	if err := json.Unmarshal([]byte(stdout), &explanations); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output = %s", err, stdout)
	}
	if len(explanations) != 2 || len(explanations[0].Evaluations) != 2 || len(explanations[1].Evaluations) != 2 {
		t.Fatalf("resource/configuration product shape = %#v", explanations)
	}
	if explanations[0].Resource.DocumentIndex != 1 || explanations[1].Resource.DocumentIndex != 2 {
		t.Errorf("resource order = %#v / %#v", explanations[0].Resource, explanations[1].Resource)
	}
	if explanations[0].Evaluations[0].Configuration.DocumentIndex != 1 || explanations[0].Evaluations[1].Configuration.DocumentIndex != 2 {
		t.Errorf("configuration order = %#v", explanations[0].Evaluations)
	}
	wantScenarioIDs := []string{
		"manifest-r0001-c0001", "manifest-r0001-c0002",
		"manifest-r0002-c0001", "manifest-r0002-c0002",
	}
	gotScenarioIDs := []string{
		explanations[0].Evaluations[0].Result.ScenarioID,
		explanations[0].Evaluations[1].Result.ScenarioID,
		explanations[1].Evaluations[0].Result.ScenarioID,
		explanations[1].Evaluations[1].Result.ScenarioID,
	}
	if strings.Join(gotScenarioIDs, ",") != strings.Join(wantScenarioIDs, ",") {
		t.Errorf("Scenario order = %v, want %v", gotScenarioIDs, wantScenarioIDs)
	}
	if calls != 0 {
		t.Errorf("offline resource hydration factory calls = %d, want zero", calls)
	}
}

func TestUniversalExplainSupportsResourceStdinAndLexicalDirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	configurationPath := filepath.Join(directory, "webhook.yaml")
	writeCLIFile(t, configurationPath, validatingCLIConfiguration("policy.example.com"))
	resourceDirectory := filepath.Join(directory, "resources")
	if err := os.Mkdir(resourceDirectory, 0o700); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}
	writeCLIFile(t, filepath.Join(resourceDirectory, "20-second.yaml"), `apiVersion: v1
kind: ConfigMap
metadata: {name: second, namespace: team-a}
`)
	writeCLIFile(t, filepath.Join(resourceDirectory, "10-first.yaml"), `apiVersion: v1
kind: Pod
metadata: {name: first, namespace: team-a}
`)

	for _, test := range []struct {
		name  string
		args  []string
		stdin string
		want  []string
	}{
		{
			name: "stdin",
			args: []string{"explain", "-f", "-", "--webhook-config", configurationPath, "-o", "json"},
			stdin: `apiVersion: v1
kind: Pod
metadata: {name: stdin-pod, namespace: team-a}
`,
			want: []string{"stdin"},
		},
		{
			name: "directory",
			args: []string{"explain", "-f", resourceDirectory, "--webhook-config", configurationPath, "-o", "json"},
			want: []string{"10-first.yaml", "20-second.yaml"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			code, stdout, stderr := executeManifestExplain(t, test.args, test.stdin, &calls)
			if code != ExitSuccess || stderr != "" {
				t.Fatalf("Execute() = code %d, stderr %q", code, stderr)
			}
			labels := explanationLabels(t, stdout)
			if strings.Join(labels, ",") != strings.Join(test.want, ",") {
				t.Errorf("resource labels = %v, want %v", labels, test.want)
			}
			if calls != 0 {
				t.Errorf("hydration factory calls = %d, want zero", calls)
			}
		})
	}
}

func TestUniversalExplainRejectsInvalidModeAndOperationCombinations(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	resourcePath := filepath.Join(directory, "resource.yaml")
	configurationPath := filepath.Join(directory, "webhook.yaml")
	scenarioPath := filepath.Join(directory, "scenario.yaml")
	writeCLIFile(t, resourcePath, `apiVersion: v1
kind: Pod
metadata: {name: example, namespace: team-a}
`)
	writeCLIFile(t, configurationPath, validatingCLIConfiguration("policy.example.com"))
	writeCLIFile(t, scenarioPath, internalExplainScenario())

	tests := []struct {
		name      string
		args      []string
		stdin     string
		want      string
		wantUsage bool
	}{
		{name: "missing configuration", args: []string{"explain", "--resource", resourcePath}, want: "requires --webhook-config or --context", wantUsage: true},
		{name: "both primary flags", args: []string{"explain", "-f", resourcePath, "--resource", resourcePath, "--webhook-config", configurationPath}, want: "cannot be used together", wantUsage: true},
		{name: "Scenario through resource", args: []string{"explain", "--resource", scenarioPath, "--webhook-config", configurationPath}, want: "not a Scenario"},
		{name: "non CREATE", args: []string{"explain", "--resource", resourcePath, "--webhook-config", configurationPath, "--operation", "UPDATE"}, want: "only CREATE", wantUsage: true},
		{
			name:  "mixed multi document",
			args:  []string{"explain", "-f", "-", "--webhook-config", configurationPath},
			stdin: internalExplainScenario() + "---\napiVersion: v1\nkind: Pod\nmetadata: {name: mixed}\n",
			want:  "stdin document 1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			code, stdout, stderr := executeManifestExplain(t, test.args, test.stdin, &calls)
			if code != ExitInvalidInput {
				t.Fatalf("Execute() = %d, want %d; stderr = %q", code, ExitInvalidInput, stderr)
			}
			if stdout != "" || !strings.Contains(stderr, test.want) {
				t.Errorf("output = (%q, %q), want error containing %q", stdout, stderr, test.want)
			}
			if got := strings.Contains(stderr, "Usage:"); got != test.wantUsage {
				t.Errorf("usage present = %t, want %t; stderr = %q", got, test.wantUsage, stderr)
			}
		})
	}
}

func TestContextModeUsesOnlyResolverReaderSurface(t *testing.T) {
	t.Parallel()

	resource := `apiVersion: v1
kind: Pod
metadata: {name: example, namespace: team-a}
`
	configuration := configurationInputsForCLI(t, validatingCLIConfigurationWithSelector())
	reader := &cliFakeReader{
		discovery: hydration.DiscoveryResult{Status: hydration.ReadStatusSuccess, Resources: []resourcecatalog.Resource{
			{Version: "v1", Kind: "Pod", Resource: "pods", Namespaced: true},
		}},
		validating: hydration.ValidatingConfigurationsResult{
			Status:         hydration.ReadStatusSuccess,
			Configurations: []admissionregistrationv1.ValidatingWebhookConfiguration{*configuration.Validating},
		},
		mutating: hydration.MutatingConfigurationsResult{Status: hydration.ReadStatusEmpty},
		namespace: hydration.NamespaceResult{
			Status:    hydration.ReadStatusSuccess,
			Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Labels: map[string]string{"environment": "prod"}}},
		},
	}
	prepareCalls := 0
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := executeWithDependencies(
		[]string{"explain", "--resource", "-", "--context", "staging", "--kubeconfig", "/logical/config", "-o", "json"},
		strings.NewReader(resource),
		&stdout,
		&stderr,
		BuildMetadata{},
		commandDependencies{prepareHydration: func(_ context.Context, options hydration.Options) (*adapter.Hydration, error) {
			prepareCalls++
			if options.Context != "staging" || options.KubeconfigPath != "/logical/config" {
				t.Errorf("hydration options = %#v", options)
			}
			return &adapter.Hydration{
				Reader:      reader,
				SourceLabel: "context:staging",
				ProfileMatch: manifest.ProfileMatch{
					Status:                    manifest.ProfileMatchVerified,
					Profile:                   contract.Kubernetes136DefaultProfile(),
					ObservedKubernetesVersion: "1.36.2",
				},
			}, nil
		}},
	)
	if code != ExitSuccess || stderr.String() != "" {
		t.Fatalf("Execute(context) = code %d, stderr %q", code, stderr.String())
	}
	if prepareCalls != 1 || reader.discoveryCalls != 1 || reader.validatingCalls != 1 || reader.mutatingCalls != 1 || reader.namespaceCalls != 1 {
		t.Errorf("context reads = prepare %d, discovery %d, validating %d, mutating %d, namespace %d; want all one", prepareCalls, reader.discoveryCalls, reader.validatingCalls, reader.mutatingCalls, reader.namespaceCalls)
	}
}

func executeManifestExplain(t *testing.T, args []string, stdin string, connectorCalls *int) (ExitCode, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := executeWithDependencies(args, strings.NewReader(stdin), &stdout, &stderr, BuildMetadata{}, commandDependencies{
		prepareHydration: func(context.Context, hydration.Options) (*adapter.Hydration, error) {
			(*connectorCalls)++
			return nil, nil
		},
	})
	return code, stdout.String(), stderr.String()
}

func explanationLabels(t *testing.T, output string) []string {
	t.Helper()
	var multiple []manifest.ManifestExplanation
	if err := json.Unmarshal([]byte(output), &multiple); err == nil {
		labels := make([]string, len(multiple))
		for index := range multiple {
			labels[index] = multiple[index].Resource.Label
		}
		return labels
	}
	var single manifest.ManifestExplanation
	if err := json.Unmarshal([]byte(output), &single); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output = %s", err, output)
	}
	return []string{single.Resource.Label}
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", filepath.Base(path), err)
	}
}

func explainLegacyBytes(ctx context.Context, data []byte) (contract.EvaluationResult, error) {
	input, err := scenario.Decode(data)
	if err != nil {
		return contract.EvaluationResult{}, err
	}
	return evaluateLegacyScenario(ctx, input)
}

func validatingCLIConfiguration(name string) string {
	return `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata: {name: validating}
webhooks:
  - name: ` + name + `
    rules:
      - operations: [CREATE]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
`
}

func mutatingCLIConfiguration(name string) string {
	return `apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata: {name: mutating}
webhooks:
  - name: ` + name + `
    rules:
      - operations: [CREATE]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
`
}

func internalExplainScenario() string {
	return `apiVersion: admitrace.io/v1alpha1
kind: Scenario
metadata: {name: legacy}
compatibilityProfile:
  id: kubernetes-1.36.2-defaults
  kubernetesVersion: 1.36.2
  featureGatePolicy: kubernetes-defaults
configuration:
  validatingWebhookConfiguration:
    apiVersion: admissionregistration.k8s.io/v1
    kind: ValidatingWebhookConfiguration
    webhooks:
      - name: policy.example.com
        rules:
          - operations: [CREATE]
            apiGroups: [""]
            apiVersions: [v1]
            resources: [pods]
request:
  kind: {version: v1, kind: Pod}
  resource: {version: v1, resource: pods}
  namespace: default
  operation: CREATE
  scope: Namespaced
  userInfo: {username: alice}
`
}

type cliFakeReader struct {
	discovery  hydration.DiscoveryResult
	validating hydration.ValidatingConfigurationsResult
	mutating   hydration.MutatingConfigurationsResult
	namespace  hydration.NamespaceResult

	discoveryCalls  int
	validatingCalls int
	mutatingCalls   int
	namespaceCalls  int
}

func (reader *cliFakeReader) Discover() hydration.DiscoveryResult {
	reader.discoveryCalls++
	return reader.discovery
}

func (reader *cliFakeReader) GetNamespace(context.Context, string) hydration.NamespaceResult {
	reader.namespaceCalls++
	return reader.namespace
}

func (reader *cliFakeReader) ListValidatingConfigurations(context.Context) hydration.ValidatingConfigurationsResult {
	reader.validatingCalls++
	return reader.validating
}

func (reader *cliFakeReader) ListMutatingConfigurations(context.Context) hydration.MutatingConfigurationsResult {
	reader.mutatingCalls++
	return reader.mutating
}

func configurationInputsForCLI(t *testing.T, input string) manifest.ConfigurationInput {
	t.Helper()
	decoded, err := manifest.Decode(strings.NewReader(input), manifest.SourceKindFile, "configuration.yaml")
	if err != nil {
		t.Fatalf("manifest.Decode() error = %v", err)
	}
	configuration, err := manifest.ConfigurationFromDocument(decoded.Documents[0])
	if err != nil {
		t.Fatalf("ConfigurationFromDocument() error = %v", err)
	}
	return configuration
}

func validatingCLIConfigurationWithSelector() string {
	return `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata: {name: validating}
webhooks:
  - name: policy.example.com
    namespaceSelector:
      matchLabels: {environment: prod}
    rules:
      - operations: [CREATE]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
`
}
