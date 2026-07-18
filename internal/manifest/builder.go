package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/resourcecatalog"
	"github.com/silbaram/admitrace/internal/scenario"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ResolutionSource identifies the trusted origin of an exact GVK mapping.
type ResolutionSource string

const (
	// ResolutionSourceBuiltInCatalog identifies the pinned offline catalog.
	ResolutionSourceBuiltInCatalog ResolutionSource = "built-in-catalog"
	// ResolutionSourceVerifiedDiscovery identifies exact-profile API discovery.
	ResolutionSourceVerifiedDiscovery ResolutionSource = "verified-discovery"
)

// IsValid reports whether source belongs to the resolution vocabulary.
func (source ResolutionSource) IsValid() bool {
	return source == ResolutionSourceBuiltInCatalog || source == ResolutionSourceVerifiedDiscovery
}

// DiscoveryResolution records one exact GVK mapping and its trusted source.
type DiscoveryResolution struct {
	GVK         schema.GroupVersionKind
	GVR         schema.GroupVersionResource
	Namespaced  bool
	Source      ResolutionSource
	SourceLabel string
}

// Resolver resolves only exact GVK mappings. Implementations must not infer a
// plural resource name or scope.
type Resolver interface {
	Resolve(schema.GroupVersionKind) (DiscoveryResolution, error)
}

// OfflineResolver resolves only the embedded Kubernetes 1.36.2 catalog.
type OfflineResolver struct{}

// Resolve implements exact offline built-in lookup.
func (OfflineResolver) Resolve(gvk schema.GroupVersionKind) (DiscoveryResolution, error) {
	resource, ok := kube136.LookupResource(gvk)
	if !ok {
		return DiscoveryResolution{}, unsupportedGVK(gvk, "not present in the embedded Kubernetes 1.36.2 catalog; use verified discovery for CRDs")
	}
	return DiscoveryResolution{
		GVK:         gvk,
		GVR:         resource.GVR(),
		Namespaced:  resource.Namespaced,
		Source:      ResolutionSourceBuiltInCatalog,
		SourceLabel: kube136.ProfileID,
	}, nil
}

// VerifiedDiscoveryResolver is an immutable exact-profile discovery snapshot.
type VerifiedDiscoveryResolver struct {
	sourceLabel string
	resources   map[schema.GroupVersionKind]resourcecatalog.Resource
}

// NewVerifiedDiscoveryResolver builds an exact lookup from verified discovery
// entries. Duplicate GVKs and an empty logical source are rejected.
func NewVerifiedDiscoveryResolver(sourceLabel string, resources []resourcecatalog.Resource) (*VerifiedDiscoveryResolver, error) {
	if sourceLabel == "" {
		return nil, &contract.InvalidInputError{Field: "sourceLabel", Err: errors.New("verified discovery source is required")}
	}
	indexed := make(map[schema.GroupVersionKind]resourcecatalog.Resource, len(resources))
	for _, resource := range resources {
		if resource.Version == "" || resource.Kind == "" || resource.Resource == "" {
			return nil, &contract.InvalidInputError{Field: "resources", Err: errors.New("discovery entry has an empty version, kind, or resource")}
		}
		if strings.Contains(resource.Resource, "/") {
			return nil, &contract.InvalidInputError{Field: "resources", Err: fmt.Errorf("discovery entry %s is a subresource", resource.GVR())}
		}
		if _, exists := indexed[resource.GVK()]; exists {
			return nil, &contract.InvalidInputError{Field: "resources", Err: fmt.Errorf("duplicate discovery GVK %s", resource.GVK())}
		}
		indexed[resource.GVK()] = resource
	}
	return &VerifiedDiscoveryResolver{sourceLabel: sourceLabel, resources: indexed}, nil
}

// Resolve implements exact verified-discovery lookup.
func (resolver *VerifiedDiscoveryResolver) Resolve(gvk schema.GroupVersionKind) (DiscoveryResolution, error) {
	if resolver == nil {
		return DiscoveryResolution{}, &contract.InvalidInputError{Field: "resolver", Err: errors.New("verified discovery resolver is required")}
	}
	resource, ok := resolver.resources[gvk]
	if !ok {
		return DiscoveryResolution{}, unsupportedGVK(gvk, "not present in verified discovery")
	}
	return DiscoveryResolution{
		GVK:         gvk,
		GVR:         resource.GVR(),
		Namespaced:  resource.Namespaced,
		Source:      ResolutionSourceVerifiedDiscovery,
		SourceLabel: resolver.sourceLabel,
	}, nil
}

// ConfigurationInput associates exactly one supported configuration with its
// logical file or verified API source.
type ConfigurationInput struct {
	Source     Source
	Validating *admissionregistrationv1.ValidatingWebhookConfiguration
	Mutating   *admissionregistrationv1.MutatingWebhookConfiguration
}

// ConfigurationFromDocument converts a typed decoder result into builder input.
func ConfigurationFromDocument(document Document) (ConfigurationInput, error) {
	configuration := ConfigurationInput{Source: document.Source}
	switch document.Class {
	case DocumentClassValidatingConfiguration:
		configuration.Validating = document.ValidatingConfiguration
	case DocumentClassMutatingConfiguration:
		configuration.Mutating = document.MutatingConfiguration
	default:
		return ConfigurationInput{}, &DocumentError{Source: document.Source, Err: &contract.InvalidInputError{Field: ".kind", Err: errors.New("document is not a supported WebhookConfiguration")}}
	}
	return configuration, nil
}

// BuildOptions contains explicit request and evaluator context. Identity is
// never inferred from kubeconfig or resolver state.
type BuildOptions struct {
	Operation       admissionv1.Operation
	Identity        Identity
	ExternalContext *contract.ExternalContext
}

// BuiltScenario associates one immutable Scenario with adapter provenance and
// its collision-free stable snapshot filename.
type BuiltScenario struct {
	Scenario         contract.Scenario
	Resource         Source
	Configuration    Source
	Resolution       DiscoveryResolution
	SnapshotName     string
	IdentityProvided bool
}

// BuildScenarios creates one Scenario for every resource×configuration pair in
// caller-provided order. It returns no partial batch on error.
func BuildScenarios(resources []Document, configurations []ConfigurationInput, resolver Resolver, options BuildOptions) ([]BuiltScenario, error) {
	if len(resources) == 0 {
		return nil, &contract.InvalidInputError{Field: "resources", Err: errors.New("at least one resource is required")}
	}
	if len(configurations) == 0 {
		return nil, &contract.InvalidInputError{Field: "configurations", Err: errors.New("at least one configuration is required")}
	}
	if resolver == nil {
		return nil, &contract.InvalidInputError{Field: "resolver", Err: errors.New("resource resolver is required")}
	}
	operation := options.Operation
	if operation == "" {
		operation = admissionv1.Create
	}
	if err := ValidateOperation(operation); err != nil {
		return nil, err
	}

	result := make([]BuiltScenario, 0, len(resources)*len(configurations))
	for resourceIndex, resource := range resources {
		if err := resource.Source.Validate(); err != nil {
			return nil, &DocumentError{Source: resource.Source, Err: &contract.InvalidInputError{Field: "source", Err: err}}
		}
		metadata, err := validateResourceMetadata(resource)
		if err != nil {
			return nil, &DocumentError{Source: resource.Source, Err: err}
		}
		resolution, err := resolver.Resolve(resource.Object.GroupVersionKind())
		if err != nil {
			return nil, &DocumentError{Source: resource.Source, Err: err}
		}
		if err := validateResolution(resource.Object.GroupVersionKind(), resolution); err != nil {
			return nil, &DocumentError{Source: resource.Source, Err: err}
		}
		if err := validateScope(metadata.namespace, resolution, options.ExternalContext); err != nil {
			return nil, &DocumentError{Source: resource.Source, Err: err}
		}

		for configurationIndex, configuration := range configurations {
			built, err := buildScenario(resource, metadata, configuration, resolution, options, resourceIndex, configurationIndex)
			if err != nil {
				return nil, err
			}
			result = append(result, built)
		}
	}
	return result, nil
}

type resourceMetadata struct {
	name      string
	namespace string
}

func validateResourceMetadata(resource Document) (resourceMetadata, error) {
	if resource.Object == nil {
		return resourceMetadata{}, &contract.InvalidInputError{Field: ".", Err: errors.New("decoded resource object is required")}
	}
	var rawObject map[string]any
	if len(resource.RawJSON) == 0 {
		return resourceMetadata{}, &contract.InvalidInputError{Field: ".", Err: errors.New("canonical resource JSON object is required")}
	}
	if err := json.Unmarshal(resource.RawJSON, &rawObject); err != nil {
		return resourceMetadata{}, &contract.InvalidInputError{Field: ".", Err: fmt.Errorf("decode canonical resource JSON: %w", err)}
	}
	if rawObject == nil {
		return resourceMetadata{}, &contract.InvalidInputError{Field: ".", Err: errors.New("canonical resource JSON must be an object")}
	}
	rawAPIVersion, _ := rawObject["apiVersion"].(string)
	rawKind, _ := rawObject["kind"].(string)
	if rawAPIVersion != resource.Object.GetAPIVersion() || rawKind != resource.Object.GetKind() {
		return resourceMetadata{}, &contract.InvalidInputError{Field: ".", Err: errors.New("decoded resource identity does not match canonical JSON")}
	}
	metadataValue, ok := rawObject["metadata"]
	if !ok {
		return resourceMetadata{}, &contract.InvalidInputError{Field: ".metadata", Err: errors.New("metadata is required")}
	}
	metadata, ok := metadataValue.(map[string]any)
	if !ok {
		return resourceMetadata{}, &contract.InvalidInputError{Field: ".metadata", Err: errors.New("metadata must be an object")}
	}
	name, err := optionalString(metadata, "name")
	if err != nil {
		return resourceMetadata{}, &contract.InvalidInputError{Field: ".metadata.name", Err: err}
	}
	generateName, err := optionalString(metadata, "generateName")
	if err != nil {
		return resourceMetadata{}, &contract.InvalidInputError{Field: ".metadata.generateName", Err: err}
	}
	if name == "" && generateName == "" {
		return resourceMetadata{}, &contract.InvalidInputError{Field: ".metadata.name", Err: errors.New("name or generateName is required")}
	}
	namespace, err := optionalString(metadata, "namespace")
	if err != nil {
		return resourceMetadata{}, &contract.InvalidInputError{Field: ".metadata.namespace", Err: err}
	}
	return resourceMetadata{name: name, namespace: namespace}, nil
}

func optionalString(object map[string]any, field string) (string, error) {
	value, ok := object[field]
	if !ok || value == nil {
		return "", nil
	}
	result, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("must be a string, got %T", value)
	}
	return result, nil
}

func validateResolution(gvk schema.GroupVersionKind, resolution DiscoveryResolution) error {
	if resolution.GVK != gvk {
		return &contract.InvalidInputError{Field: "resolution.gvk", Err: fmt.Errorf("resolver returned %s for %s", resolution.GVK, gvk)}
	}
	if resolution.GVR.Group != gvk.Group || resolution.GVR.Version != gvk.Version || resolution.GVR.Resource == "" {
		return &contract.InvalidInputError{Field: "resolution.gvr", Err: fmt.Errorf("resolver returned incompatible GVR %s for %s", resolution.GVR, gvk)}
	}
	if !resolution.Source.IsValid() || resolution.SourceLabel == "" {
		return &contract.InvalidInputError{Field: "resolution.source", Err: errors.New("trusted resolution source and label are required")}
	}
	return nil
}

func validateScope(namespace string, resolution DiscoveryResolution, external *contract.ExternalContext) error {
	if !resolution.Namespaced && namespace != "" {
		return &contract.InvalidInputError{Field: ".metadata.namespace", Err: fmt.Errorf("cluster-scoped resource %s must not set namespace %q", resolution.GVK, namespace)}
	}
	if external == nil || external.Namespace == nil {
		return nil
	}
	if !resolution.Namespaced {
		return &contract.InvalidInputError{Field: ".externalContext.namespace", Err: fmt.Errorf("cluster-scoped resource %s cannot use Namespace context", resolution.GVK)}
	}
	if namespace == "" || external.Namespace.Name != namespace {
		return &contract.InvalidInputError{Field: ".externalContext.namespace.metadata.name", Err: fmt.Errorf("Namespace context %q does not match resource namespace %q", external.Namespace.Name, namespace)}
	}
	return nil
}

func buildScenario(resource Document, metadata resourceMetadata, configuration ConfigurationInput, resolution DiscoveryResolution, options BuildOptions, resourceIndex, configurationIndex int) (BuiltScenario, error) {
	if err := configuration.Source.Validate(); err != nil {
		return BuiltScenario{}, &DocumentError{Source: configuration.Source, Err: &contract.InvalidInputError{Field: "source", Err: err}}
	}
	webhookConfiguration, err := cloneConfiguration(configuration)
	if err != nil {
		return BuiltScenario{}, &DocumentError{Source: configuration.Source, Err: err}
	}
	externalContext, err := cloneExternalContext(options.ExternalContext)
	if err != nil {
		return BuiltScenario{}, &contract.InternalError{Operation: "clone external context", Err: err}
	}
	userInfo := options.Identity.userInfoCopy()
	scope := contract.RequestScopeCluster
	if resolution.Namespaced {
		scope = contract.RequestScopeNamespaced
	}
	scenarioInput := contract.Scenario{
		APIVersion:           contract.ScenarioAPIVersion,
		Kind:                 contract.ScenarioKind,
		Metadata:             contract.ScenarioMetadata{Name: fmt.Sprintf("manifest-r%04d-c%04d", resourceIndex+1, configurationIndex+1)},
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		Configuration:        webhookConfiguration,
		Request: contract.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   resource.Object.GroupVersionKind().Group,
				Version: resource.Object.GroupVersionKind().Version,
				Kind:    resource.Object.GroupVersionKind().Kind,
			},
			Resource: metav1.GroupVersionResource{
				Group:    resolution.GVR.Group,
				Version:  resolution.GVR.Version,
				Resource: resolution.GVR.Resource,
			},
			Name:      metadata.name,
			Namespace: metadata.namespace,
			Operation: admissionv1.Create,
			Scope:     scope,
			UserInfo:  *userInfo,
			Object:    append(json.RawMessage(nil), resource.RawJSON...),
		},
		ExternalContext: externalContext,
	}
	if err := scenario.Validate(&scenarioInput); err != nil {
		return BuiltScenario{}, &DocumentError{Source: configuration.Source, Err: err}
	}
	scenario.ApplyDefaults(&scenarioInput)
	return BuiltScenario{
		Scenario:         scenarioInput,
		Resource:         resource.Source,
		Configuration:    configuration.Source,
		Resolution:       resolution,
		SnapshotName:     fmt.Sprintf("%04d-%04d.yaml", resourceIndex+1, configurationIndex+1),
		IdentityProvided: options.Identity.Provided(),
	}, nil
}

func cloneConfiguration(input ConfigurationInput) (contract.WebhookConfiguration, error) {
	if (input.Validating == nil) == (input.Mutating == nil) {
		return contract.WebhookConfiguration{}, &contract.InvalidInputError{Field: ".configuration", Err: errors.New("exactly one validating or mutating configuration is required")}
	}
	if input.Validating != nil {
		return contract.WebhookConfiguration{Validating: input.Validating.DeepCopy()}, nil
	}
	return contract.WebhookConfiguration{Mutating: input.Mutating.DeepCopy()}, nil
}

func cloneExternalContext(input *contract.ExternalContext) (*contract.ExternalContext, error) {
	if input == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var result contract.ExternalContext
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func unsupportedGVK(gvk schema.GroupVersionKind, detail string) error {
	return &contract.UnsupportedCapabilityError{Capability: "resource GVK " + gvk.String(), Err: errors.New(detail)}
}
