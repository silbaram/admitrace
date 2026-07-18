package manifest_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/resourcecatalog"
	"github.com/silbaram/admitrace/internal/scenario"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildScenariosPreservesCombinationOrder(t *testing.T) {
	t.Parallel()

	resources := decodeResourceDocuments(t, `apiVersion: v1
kind: Pod
metadata:
  name: first
  namespace: team-a
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: second
  namespace: team-a
`)
	configurations := decodeConfigurationDocuments(t, validatingConfiguration+"---\n"+mutatingConfiguration)
	options := manifest.BuildOptions{
		UserInfo: authenticationv1.UserInfo{
			Username: "alice",
			Groups:   []string{"developers"},
			Extra:    map[string]authenticationv1.ExtraValue{"tenant": {"blue"}},
		},
		ExternalContext: &contract.ExternalContext{Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a"}}},
	}

	built, err := manifest.BuildScenarios(resources, configurations, manifest.OfflineResolver{}, options)
	if err != nil {
		t.Fatalf("BuildScenarios() error = %v", err)
	}
	if len(built) != 4 {
		t.Fatalf("built scenarios = %d, want 4", len(built))
	}
	wantNames := []string{
		"manifest-r0001-c0001",
		"manifest-r0001-c0002",
		"manifest-r0002-c0001",
		"manifest-r0002-c0002",
	}
	wantSnapshots := []string{"0001-0001.yaml", "0001-0002.yaml", "0002-0001.yaml", "0002-0002.yaml"}
	gotNames := make([]string, 0, len(built))
	gotSnapshots := make([]string, 0, len(built))
	for index := range built {
		item := &built[index]
		gotNames = append(gotNames, item.Scenario.Metadata.Name)
		gotSnapshots = append(gotSnapshots, item.SnapshotName)
		if err := scenario.Validate(&item.Scenario); err != nil {
			t.Errorf("scenario %d validation error = %v", index, err)
		}
		if item.Resolution.Source != manifest.ResolutionSourceBuiltInCatalog {
			t.Errorf("scenario %d resolution source = %q, want built-in catalog", index, item.Resolution.Source)
		}
		if item.Scenario.Request.UserInfo.Username != "alice" || item.Scenario.ExternalContext.Namespace.Name != "team-a" {
			t.Errorf("scenario %d lost explicit identity or Namespace context", index)
		}
	}
	if !slices.Equal(gotNames, wantNames) {
		t.Errorf("scenario names = %v, want %v", gotNames, wantNames)
	}
	if !slices.Equal(gotSnapshots, wantSnapshots) {
		t.Errorf("snapshot names = %v, want %v", gotSnapshots, wantSnapshots)
	}
	if built[0].Configuration.Label != "configuration.yaml" || built[1].Configuration.DocumentIndex != 2 {
		t.Errorf("configuration provenance = %#v / %#v, want file document order", built[0].Configuration, built[1].Configuration)
	}
	if built[0].Scenario.Configuration.Validating == nil || built[1].Scenario.Configuration.Mutating == nil {
		t.Error("validating/mutating configuration order was not preserved")
	}
}

func TestBuildScenariosDoesNotMutateInputs(t *testing.T) {
	t.Parallel()

	resources := decodeResourceDocuments(t, `apiVersion: v1
kind: Pod
metadata: {name: immutable, namespace: team-a}
`)
	configurations := decodeConfigurationDocuments(t, validatingConfiguration)
	options := manifest.BuildOptions{
		UserInfo:        authenticationv1.UserInfo{Username: "alice", Extra: map[string]authenticationv1.ExtraValue{"tenant": {"blue"}}},
		ExternalContext: &contract.ExternalContext{Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a"}}},
	}

	built, err := manifest.BuildScenarios(resources, configurations, manifest.OfflineResolver{}, options)
	if err != nil {
		t.Fatalf("BuildScenarios() error = %v", err)
	}
	resources[0].RawJSON[0] = 'x'
	resources[0].Object.SetName("changed")
	configurations[0].Validating.Webhooks[0].Name = "changed.example.com"
	options.UserInfo.Extra["tenant"][0] = "changed"
	options.ExternalContext.Namespace.Name = "changed"

	item := built[0].Scenario
	if item.Request.Name != "immutable" || item.Configuration.Validating.Webhooks[0].Name != "validating.example.com" {
		t.Error("built Scenario changed after resource or configuration input mutation")
	}
	if item.Request.UserInfo.Extra["tenant"][0] != "blue" || item.ExternalContext.Namespace.Name != "team-a" {
		t.Error("built Scenario changed after explicit context mutation")
	}
	if !json.Valid(item.Request.Object) {
		t.Error("built Scenario raw object changed after source RawJSON mutation")
	}
}

func TestBuildScenariosUsesVerifiedDiscoveryWithoutInference(t *testing.T) {
	t.Parallel()

	resources := decodeResourceDocuments(t, `apiVersion: widgets.example.com/v1
kind: Widget
metadata:
  name: custom
  namespace: team-a
`)
	configurations := decodeConfigurationDocuments(t, validatingConfiguration)

	_, err := manifest.BuildScenarios(resources, configurations, manifest.OfflineResolver{}, manifest.BuildOptions{})
	if !errors.Is(err, contract.ErrUnsupportedCapability) {
		t.Fatalf("offline BuildScenarios() error = %v, want unsupported capability", err)
	}
	var documentError *manifest.DocumentError
	if !errors.As(err, &documentError) || documentError.Source.Label != "resource.yaml" || documentError.Source.DocumentIndex != 1 {
		t.Fatalf("offline error provenance = %#v, want resource.yaml document 1", documentError)
	}

	resolver, err := manifest.NewVerifiedDiscoveryResolver("context:staging", []resourcecatalog.Resource{{
		Group: "widgets.example.com", Version: "v1", Kind: "Widget", Resource: "widgets", Namespaced: true,
	}})
	if err != nil {
		t.Fatalf("NewVerifiedDiscoveryResolver() error = %v", err)
	}
	built, err := manifest.BuildScenarios(resources, configurations, resolver, manifest.BuildOptions{})
	if err != nil {
		t.Fatalf("verified BuildScenarios() error = %v", err)
	}
	resolution := built[0].Resolution
	if resolution.Source != manifest.ResolutionSourceVerifiedDiscovery || resolution.SourceLabel != "context:staging" || resolution.GVR.Resource != "widgets" {
		t.Errorf("resolution = %#v, want exact verified discovery", resolution)
	}
	if built[0].Scenario.Request.Resource.Resource != "widgets" || built[0].Scenario.Request.Scope != contract.RequestScopeNamespaced {
		t.Errorf("request mapping = %#v, want widgets/namespaced", built[0].Scenario.Request)
	}
}

func TestBuildScenariosRejectsInvalidMetadataAndScope(t *testing.T) {
	t.Parallel()

	configurations := decodeConfigurationDocuments(t, validatingConfiguration)
	tests := []struct {
		name      string
		manifest  string
		wantField string
	}{
		{
			name: "missing metadata",
			manifest: `apiVersion: v1
kind: Pod
`,
			wantField: ".metadata",
		},
		{
			name: "metadata wrong type",
			manifest: `apiVersion: v1
kind: Pod
metadata: invalid
`,
			wantField: ".metadata",
		},
		{
			name: "missing name",
			manifest: `apiVersion: v1
kind: Pod
metadata:
  namespace: team-a
`,
			wantField: ".metadata.name",
		},
		{
			name: "cluster resource namespace",
			manifest: `apiVersion: v1
kind: Node
metadata:
  name: node-a
  namespace: forbidden
`,
			wantField: ".metadata.namespace",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			resources := decodeResourceDocuments(t, test.manifest)
			_, err := manifest.BuildScenarios(resources, configurations, manifest.OfflineResolver{}, manifest.BuildOptions{})
			if !errors.Is(err, contract.ErrInvalidInput) {
				t.Fatalf("BuildScenarios() error = %v, want invalid input", err)
			}
			var invalid *contract.InvalidInputError
			if !errors.As(err, &invalid) || invalid.Field != test.wantField {
				t.Errorf("InvalidInputError = %#v, want field %q", invalid, test.wantField)
			}
		})
	}
}

func TestBuildScenariosPreservesFileAndClusterConfigurationSources(t *testing.T) {
	t.Parallel()

	resources := decodeResourceDocuments(t, `apiVersion: v1
kind: ConfigMap
metadata:
  name: example
  namespace: default
`)
	fileConfigurations := decodeConfigurationDocuments(t, validatingConfiguration)
	clusterConfiguration := decodeConfigurationDocuments(t, mutatingConfiguration)[0]
	clusterConfiguration.Source = manifest.Source{Kind: manifest.SourceKindCluster, Label: "context:staging"}
	configurations := append(fileConfigurations, clusterConfiguration)

	built, err := manifest.BuildScenarios(resources, configurations, manifest.OfflineResolver{}, manifest.BuildOptions{Operation: admissionv1.Create})
	if err != nil {
		t.Fatalf("BuildScenarios() error = %v", err)
	}
	if built[0].Configuration.Kind != manifest.SourceKindFile || built[0].Configuration.DocumentIndex != 1 {
		t.Errorf("file configuration source = %#v", built[0].Configuration)
	}
	if built[1].Configuration.Kind != manifest.SourceKindCluster || built[1].Configuration.Label != "context:staging" || built[1].Configuration.DocumentIndex != 0 {
		t.Errorf("cluster configuration source = %#v", built[1].Configuration)
	}
}

func TestBuildScenariosRejectsNonCreateOperation(t *testing.T) {
	t.Parallel()

	resources := decodeResourceDocuments(t, `apiVersion: v1
kind: Pod
metadata: {name: example}
`)
	configurations := decodeConfigurationDocuments(t, validatingConfiguration)
	_, err := manifest.BuildScenarios(resources, configurations, manifest.OfflineResolver{}, manifest.BuildOptions{Operation: admissionv1.Update})
	if !errors.Is(err, contract.ErrUnsupportedCapability) {
		t.Fatalf("BuildScenarios() error = %v, want unsupported capability", err)
	}
}

func decodeResourceDocuments(t *testing.T, input string) []manifest.Document {
	t.Helper()
	decoded, err := manifest.Decode(strings.NewReader(input), manifest.SourceKindFile, "resource.yaml")
	if err != nil {
		t.Fatalf("manifest.Decode(resource) error = %v", err)
	}
	if decoded.Mode != manifest.InputModeResource {
		t.Fatalf("manifest.Decode(resource) mode = %q, want resource", decoded.Mode)
	}
	return decoded.Documents
}

func decodeConfigurationDocuments(t *testing.T, input string) []manifest.ConfigurationInput {
	t.Helper()
	decoded, err := manifest.Decode(strings.NewReader(input), manifest.SourceKindFile, "configuration.yaml")
	if err != nil {
		t.Fatalf("manifest.Decode(configuration) error = %v", err)
	}
	result := make([]manifest.ConfigurationInput, 0, len(decoded.Documents))
	for _, document := range decoded.Documents {
		configuration, err := manifest.ConfigurationFromDocument(document)
		if err != nil {
			t.Fatalf("ConfigurationFromDocument() error = %v", err)
		}
		result = append(result, configuration)
	}
	return result
}

const validatingConfiguration = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: validating
webhooks:
  - name: validating.example.com
    rules:
      - operations: [CREATE]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
`

const mutatingConfiguration = `apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: mutating
webhooks:
  - name: mutating.example.com
    rules:
      - operations: [CREATE]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
`
