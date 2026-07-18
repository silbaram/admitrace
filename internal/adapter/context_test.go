package adapter_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/adapter"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/hydration"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/resourcecatalog"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResolveUsesExplicitFilesBeforeClusterReads(t *testing.T) {
	t.Parallel()

	resource := decodeDocuments(t, `apiVersion: v1
kind: Pod
metadata: {name: example, namespace: team-a}
`)[0]
	configurations := configurationInputs(t, validatingWithNamespaceSelector)
	namespace := decodeDocuments(t, `apiVersion: v1
kind: Namespace
metadata:
  name: team-a
  labels: {environment: prod}
`)[0]
	reader := &fakeReader{discovery: successfulDiscovery(corePodResource())}

	result, err := adapter.Resolve(context.Background(), []manifest.Document{resource}, adapter.Options{
		FileConfigurations: configurations,
		FileNamespace:      &namespace,
		Hydration:          verifiedHydration(reader),
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if reader.validatingCalls != 0 || reader.mutatingCalls != 0 || reader.namespaceCalls != 0 {
		t.Fatalf("file-first cluster reads = validating %d, mutating %d, namespace %d; want zero", reader.validatingCalls, reader.mutatingCalls, reader.namespaceCalls)
	}
	if reader.discoveryCalls != 1 {
		t.Errorf("discovery calls = %d, want one exact resolver read", reader.discoveryCalls)
	}
	if len(result.BuiltScenarios) != 1 || result.BuiltScenarios[0].Scenario.ExternalContext.Namespace.Labels["environment"] != "prod" {
		t.Fatalf("built scenarios = %#v, want explicit Namespace fixture", result.BuiltScenarios)
	}
	completeness := result.Resources[0].Completeness
	if completeness.Configuration.Status != manifest.CompletenessProvided || completeness.Namespace.Status != manifest.CompletenessProvided || completeness.Discovery.Status != manifest.CompletenessHydrated {
		t.Errorf("file-first completeness = %#v", completeness)
	}
}

func TestResolveHydratesNamespaceOnlyWhenSelectedConfigurationNeedsIt(t *testing.T) {
	t.Parallel()

	resource := decodeDocuments(t, `apiVersion: v1
kind: Pod
metadata: {name: example, namespace: team-a}
`)[0]
	selectorConfiguration := configurationInputs(t, validatingWithNamespaceSelector)[0].Validating.DeepCopy()
	reader := &fakeReader{
		discovery: successfulDiscovery(corePodResource()),
		validating: hydration.ValidatingConfigurationsResult{
			Status:         hydration.ReadStatusSuccess,
			Configurations: []admissionregistrationv1.ValidatingWebhookConfiguration{*selectorConfiguration},
		},
		mutating: hydration.MutatingConfigurationsResult{Status: hydration.ReadStatusEmpty},
		namespace: hydration.NamespaceResult{
			Status:    hydration.ReadStatusSuccess,
			Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Labels: map[string]string{"environment": "prod"}}},
		},
	}
	result, err := adapter.Resolve(context.Background(), []manifest.Document{resource}, adapter.Options{Hydration: verifiedHydration(reader)})
	if err != nil {
		t.Fatalf("Resolve(selector) error = %v", err)
	}
	if reader.validatingCalls != 1 || reader.mutatingCalls != 1 || reader.namespaceCalls != 1 {
		t.Errorf("selector reads = validating %d, mutating %d, namespace %d; want 1/1/1", reader.validatingCalls, reader.mutatingCalls, reader.namespaceCalls)
	}
	if result.Resources[0].Completeness.Namespace.Status != manifest.CompletenessHydrated || result.BuiltScenarios[0].Scenario.ExternalContext.Namespace.Name != "team-a" {
		t.Errorf("hydrated Namespace result = %#v / %#v", result.Resources[0], result.BuiltScenarios[0].Scenario.ExternalContext)
	}

	withoutSelector := configurationInputs(t, validatingWithoutNamespaceSelector)[0].Validating.DeepCopy()
	reader = &fakeReader{
		discovery:  successfulDiscovery(corePodResource()),
		validating: hydration.ValidatingConfigurationsResult{Status: hydration.ReadStatusSuccess, Configurations: []admissionregistrationv1.ValidatingWebhookConfiguration{*withoutSelector}},
		mutating:   hydration.MutatingConfigurationsResult{Status: hydration.ReadStatusEmpty},
		namespace:  hydration.NamespaceResult{Status: hydration.ReadStatusUnavailable, Err: errors.New("must not be read")},
	}
	result, err = adapter.Resolve(context.Background(), []manifest.Document{resource}, adapter.Options{Hydration: verifiedHydration(reader)})
	if err != nil {
		t.Fatalf("Resolve(no selector) error = %v", err)
	}
	if reader.namespaceCalls != 0 {
		t.Errorf("Namespace GET calls without selector = %d, want zero", reader.namespaceCalls)
	}
	if result.Resources[0].Completeness.Namespace.Status != manifest.CompletenessNotRequired {
		t.Errorf("no-selector Namespace completeness = %q, want not-required", result.Resources[0].Completeness.Namespace.Status)
	}
}

func TestResolveUsesVerifiedDiscoveryForCRDButOfflineDoesNotGuess(t *testing.T) {
	t.Parallel()

	resource := decodeDocuments(t, `apiVersion: widgets.example.com/v1
kind: Widget
metadata: {name: example, namespace: team-a}
`)[0]
	configurations := configurationInputs(t, validatingWithoutNamespaceSelector)
	_, err := adapter.Resolve(context.Background(), []manifest.Document{resource}, adapter.Options{FileConfigurations: configurations})
	if !errors.Is(err, contract.ErrUnsupportedCapability) {
		t.Fatalf("offline Resolve(CRD) error = %v, want unsupported capability", err)
	}

	reader := &fakeReader{discovery: successfulDiscovery(resourcecatalog.Resource{
		Group: "widgets.example.com", Version: "v1", Kind: "Widget", Resource: "widgets", Namespaced: true,
	})}
	result, err := adapter.Resolve(context.Background(), []manifest.Document{resource}, adapter.Options{
		FileConfigurations: configurations,
		Hydration:          verifiedHydration(reader),
	})
	if err != nil {
		t.Fatalf("hydrated Resolve(CRD) error = %v", err)
	}
	if len(result.BuiltScenarios) != 1 || result.BuiltScenarios[0].Scenario.Request.Resource.Resource != "widgets" || result.BuiltScenarios[0].Resolution.Source != manifest.ResolutionSourceVerifiedDiscovery {
		t.Errorf("hydrated CRD result = %#v", result.BuiltScenarios)
	}
}

func TestResolvePreservesDiscoveryAndConfigurationFailuresWithoutFallback(t *testing.T) {
	t.Parallel()

	resource := decodeDocuments(t, `apiVersion: v1
kind: Pod
metadata: {name: example, namespace: team-a}
`)[0]
	reader := &fakeReader{
		discovery:  hydration.DiscoveryResult{Status: hydration.ReadStatusForbidden, Err: errors.New("denied")},
		validating: hydration.ValidatingConfigurationsResult{Status: hydration.ReadStatusSuccess},
		mutating:   hydration.MutatingConfigurationsResult{Status: hydration.ReadStatusSuccess},
	}
	result, err := adapter.Resolve(context.Background(), []manifest.Document{resource}, adapter.Options{Hydration: verifiedHydration(reader)})
	if err != nil {
		t.Fatalf("Resolve(discovery forbidden) error = %v", err)
	}
	if reader.validatingCalls != 0 || reader.mutatingCalls != 0 || reader.namespaceCalls != 0 {
		t.Errorf("reads after discovery failure = %d/%d/%d, want zero", reader.validatingCalls, reader.mutatingCalls, reader.namespaceCalls)
	}
	if result.Resources[0].Completeness.Discovery.Status != manifest.CompletenessForbidden || result.Resources[0].Completeness.Configuration.Status != manifest.CompletenessMissing || len(result.BuiltScenarios) != 0 {
		t.Errorf("discovery failure result = %#v", result)
	}

	mutating := configurationInputs(t, mutatingWithoutNamespaceSelector)[0].Mutating.DeepCopy()
	reader = &fakeReader{
		discovery:  successfulDiscovery(corePodResource()),
		validating: hydration.ValidatingConfigurationsResult{Status: hydration.ReadStatusForbidden, Err: errors.New("denied")},
		mutating: hydration.MutatingConfigurationsResult{
			Status:         hydration.ReadStatusSuccess,
			Configurations: []admissionregistrationv1.MutatingWebhookConfiguration{*mutating},
		},
	}
	result, err = adapter.Resolve(context.Background(), []manifest.Document{resource}, adapter.Options{Hydration: verifiedHydration(reader)})
	if err != nil {
		t.Fatalf("Resolve(partial configuration) error = %v", err)
	}
	if len(result.BuiltScenarios) != 1 || result.Resources[0].Completeness.Configuration.Status != manifest.CompletenessForbidden {
		t.Errorf("partial configuration result = %#v", result)
	}
}

func TestResolveReportsMissingOfflineConfigurationPerResource(t *testing.T) {
	t.Parallel()

	resource := decodeDocuments(t, `apiVersion: v1
kind: Pod
metadata: {name: example, namespace: team-a}
`)[0]
	result, err := adapter.Resolve(context.Background(), []manifest.Document{resource}, adapter.Options{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.Resources[0].Completeness.Configuration.Status != manifest.CompletenessMissing || len(result.BuiltScenarios) != 0 {
		t.Errorf("missing configuration result = %#v", result)
	}
	if len(result.Resources[0].Diagnostics) != 1 || result.Resources[0].Diagnostics[0].SourceLabel != resource.Source.Label || result.Resources[0].Diagnostics[0].DocumentIndex != resource.Source.DocumentIndex {
		t.Errorf("source-addressed diagnostic = %#v", result.Resources[0].Diagnostics)
	}
}

func TestResolvePreservesNamespacePermissionStatusAndFailClosedScenario(t *testing.T) {
	t.Parallel()

	resource := decodeDocuments(t, `apiVersion: v1
kind: Pod
metadata: {name: example, namespace: team-a}
`)[0]
	configurations := configurationInputs(t, validatingWithNamespaceSelector)
	reader := &fakeReader{
		discovery: successfulDiscovery(corePodResource()),
		namespace: hydration.NamespaceResult{Status: hydration.ReadStatusForbidden, Err: errors.New("denied")},
	}
	result, err := adapter.Resolve(context.Background(), []manifest.Document{resource}, adapter.Options{
		FileConfigurations: configurations,
		Hydration:          verifiedHydration(reader),
	})
	if err != nil {
		t.Fatalf("Resolve(namespace forbidden) error = %v", err)
	}
	if result.Resources[0].Completeness.Namespace.Status != manifest.CompletenessForbidden {
		t.Errorf("Namespace completeness = %q, want forbidden", result.Resources[0].Completeness.Namespace.Status)
	}
	if result.BuiltScenarios[0].Scenario.ExternalContext != nil && result.BuiltScenarios[0].Scenario.ExternalContext.Namespace != nil {
		t.Error("forbidden Namespace was synthesized into Scenario")
	}
	if len(result.Resources[0].Diagnostics) == 0 || !strings.Contains(result.Resources[0].Diagnostics[len(result.Resources[0].Diagnostics)-1].Message, "--namespace-object") {
		t.Errorf("Namespace fallback diagnostic = %#v", result.Resources[0].Diagnostics)
	}
	evaluation, err := manifest.EvaluateBuiltScenario(context.Background(), result.BuiltScenarios[0])
	if err != nil {
		t.Fatalf("EvaluateBuiltScenario() error = %v", err)
	}
	if len(evaluation.Result.Webhooks) != 1 || len(evaluation.Result.Webhooks[0].Diagnostics) == 0 || evaluation.Result.Webhooks[0].Diagnostics[0].Code != contract.ReasonCodeNamespaceContextMissing {
		t.Errorf("forbidden Namespace evaluation = %#v, want fail-closed missing-context", evaluation.Result)
	}
}

func TestFinalizeCompletenessMarksOnlyReachedFailClosedDependencies(t *testing.T) {
	t.Parallel()

	base := manifest.ContextCompleteness{
		Configuration: manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: "configuration.yaml"},
		Discovery:     manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: "kubernetes-1.36.2-defaults"},
		Namespace:     manifest.ContextStatus{Status: manifest.CompletenessForbidden},
		Identity:      manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
		Equivalence:   manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
		Authorization: manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
	}
	evaluation := manifest.AdapterEvaluation{Result: contract.EvaluationResult{Webhooks: []contract.WebhookEvaluation{{
		Diagnostics: []contract.Diagnostic{
			{Code: contract.ReasonCodeNamespaceContextMissing},
			{Code: contract.ReasonCodeIdentityContextMissing},
			{Code: contract.ReasonCodeEquivalenceContextMissing},
			{Code: contract.ReasonCodeAuthorizationContextMissing},
		},
	}}}}
	got := adapter.FinalizeCompleteness(base, []manifest.AdapterEvaluation{evaluation})
	if got.Namespace.Status != manifest.CompletenessForbidden {
		t.Errorf("Namespace status = %q, want preserved forbidden", got.Namespace.Status)
	}
	if got.Identity.Status != manifest.CompletenessMissing || got.Equivalence.Status != manifest.CompletenessMissing || got.Authorization.Status != manifest.CompletenessMissing {
		t.Errorf("finalized optional contexts = %#v", got)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("finalized completeness validation error = %v", err)
	}
}

type fakeReader struct {
	discovery  hydration.DiscoveryResult
	validating hydration.ValidatingConfigurationsResult
	mutating   hydration.MutatingConfigurationsResult
	namespace  hydration.NamespaceResult

	discoveryCalls  int
	validatingCalls int
	mutatingCalls   int
	namespaceCalls  int
}

func (reader *fakeReader) Discover() hydration.DiscoveryResult {
	reader.discoveryCalls++
	return reader.discovery
}

func (reader *fakeReader) GetNamespace(context.Context, string) hydration.NamespaceResult {
	reader.namespaceCalls++
	return reader.namespace
}

func (reader *fakeReader) ListValidatingConfigurations(context.Context) hydration.ValidatingConfigurationsResult {
	reader.validatingCalls++
	return reader.validating
}

func (reader *fakeReader) ListMutatingConfigurations(context.Context) hydration.MutatingConfigurationsResult {
	reader.mutatingCalls++
	return reader.mutating
}

func verifiedHydration(reader adapter.Reader) *adapter.Hydration {
	return &adapter.Hydration{
		Reader:      reader,
		SourceLabel: "context:staging",
		ProfileMatch: manifest.ProfileMatch{
			Status:                    manifest.ProfileMatchVerified,
			Profile:                   contract.Kubernetes136DefaultProfile(),
			ObservedKubernetesVersion: "1.36.2",
		},
	}
}

func successfulDiscovery(resources ...resourcecatalog.Resource) hydration.DiscoveryResult {
	return hydration.DiscoveryResult{Status: hydration.ReadStatusSuccess, Resources: resources}
}

func corePodResource() resourcecatalog.Resource {
	return resourcecatalog.Resource{Version: "v1", Kind: "Pod", Resource: "pods", Namespaced: true}
}

func decodeDocuments(t *testing.T, input string) []manifest.Document {
	t.Helper()
	decoded, err := manifest.Decode(strings.NewReader(input), manifest.SourceKindFile, "input.yaml")
	if err != nil {
		t.Fatalf("manifest.Decode() error = %v", err)
	}
	return decoded.Documents
}

func configurationInputs(t *testing.T, input string) []manifest.ConfigurationInput {
	t.Helper()
	documents := decodeDocuments(t, input)
	result := make([]manifest.ConfigurationInput, 0, len(documents))
	for _, document := range documents {
		configuration, err := manifest.ConfigurationFromDocument(document)
		if err != nil {
			t.Fatalf("ConfigurationFromDocument() error = %v", err)
		}
		result = append(result, configuration)
	}
	return result
}

const validatingWithoutNamespaceSelector = `apiVersion: admissionregistration.k8s.io/v1
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

const validatingWithNamespaceSelector = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata: {name: validating}
webhooks:
  - name: validating.example.com
    namespaceSelector:
      matchLabels: {environment: prod}
    rules:
      - operations: [CREATE]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
`

const mutatingWithoutNamespaceSelector = `apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata: {name: mutating}
webhooks:
  - name: mutating.example.com
    rules:
      - operations: [CREATE]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
`
