// Package adapter resolves manifest-mode inputs without weakening the offline
// evaluator's fail-closed context semantics.
package adapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/hydration"
	"github.com/silbaram/admitrace/internal/manifest"
	corev1 "k8s.io/api/core/v1"
)

const (
	fileConfigurationSourceLabel = "explicit-webhook-configuration"
	explicitIdentitySourceLabel  = "explicit-admission-user"
)

// Reader is the complete Kubernetes read surface available to context
// resolution. Implementations are expected to come from a verified hydration
// Session.
type Reader interface {
	Discover() hydration.DiscoveryResult
	GetNamespace(context.Context, string) hydration.NamespaceResult
	ListValidatingConfigurations(context.Context) hydration.ValidatingConfigurationsResult
	ListMutatingConfigurations(context.Context) hydration.MutatingConfigurationsResult
}

// Hydration identifies an explicitly selected, exact-profile Kubernetes
// reader. SourceLabel is logical provenance such as context:name, never a
// server URL or credential-bearing value.
type Hydration struct {
	Reader       Reader
	SourceLabel  string
	ProfileMatch manifest.ProfileMatch
}

// Options contains explicit file inputs and optional verified hydration.
// A non-nil FileConfigurations slice means --webhook-config was supplied,
// including when decoding yielded no usable configuration. FileNamespace nil
// means --namespace-object was not supplied.
type Options struct {
	FileConfigurations       []manifest.ConfigurationInput
	FileNamespace            *manifest.Document
	Hydration                *Hydration
	Identity                 manifest.Identity
	ExternalContext          *contract.ExternalContext
	EquivalenceProvided      bool
	EquivalenceSourceLabel   string
	AuthorizationProvided    bool
	AuthorizationSourceLabel string
}

// ResourceContext records source-specific completeness and diagnostics for one
// input resource.
type ResourceContext struct {
	Resource     manifest.Source
	Completeness manifest.ContextCompleteness
	Diagnostics  []manifest.Diagnostic
}

// Result contains deterministic builder outputs and their adapter context.
type Result struct {
	ProfileMatch   manifest.ProfileMatch
	BuiltScenarios []manifest.BuiltScenario
	Resources      []ResourceContext
}

// Resolve builds immutable Scenarios using file-first source precedence.
// Source acquisition failures are preserved in Result rather than converted
// into guessed inputs; invalid supplied inputs still return an error.
func Resolve(ctx context.Context, resources []manifest.Document, options Options) (Result, error) {
	if ctx == nil {
		return Result{}, &contract.InvalidInputError{Field: "context", Err: errors.New("context.Context is required")}
	}
	if len(resources) == 0 {
		return Result{}, &contract.InvalidInputError{Field: "resources", Err: errors.New("at least one resource is required")}
	}
	profileMatch, resolver, discoveryStatus, discoveryDiagnostics, discoveryReady, err := resolveDiscovery(options.Hydration)
	if err != nil {
		return Result{}, err
	}
	configurations, configurationStatus, configurationDiagnostics := resolveConfigurations(ctx, options, discoveryReady)

	base := manifest.ContextCompleteness{
		Configuration: configurationStatus,
		Discovery:     discoveryStatus,
		Identity:      optionalContextStatus(options.Identity.Provided(), explicitIdentitySourceLabel),
		Equivalence: optionalContextStatus(
			options.EquivalenceProvided || (options.ExternalContext != nil && len(options.ExternalContext.Equivalence) > 0),
			logicalSource(options.EquivalenceSourceLabel, "explicit-equivalence"),
		),
		Authorization: optionalContextStatus(
			options.AuthorizationProvided || (options.ExternalContext != nil && len(options.ExternalContext.Authorization) > 0),
			logicalSource(options.AuthorizationSourceLabel, "explicit-authorization"),
		),
	}

	result := Result{ProfileMatch: profileMatch, Resources: make([]ResourceContext, len(resources))}
	for index, resource := range resources {
		result.Resources[index] = ResourceContext{
			Resource:     resource.Source,
			Completeness: base,
			Diagnostics:  diagnosticsForResource(resource.Source, discoveryDiagnostics, configurationDiagnostics),
		}
	}
	if resolver == nil || len(configurations) == 0 {
		for index := range result.Resources {
			result.Resources[index].Completeness.Namespace = manifest.ContextStatus{Status: manifest.CompletenessNotRequired}
			if err := result.Resources[index].Completeness.Validate(); err != nil {
				return Result{}, &contract.InternalError{Operation: "validate unresolved adapter completeness", Err: err}
			}
		}
		return result, nil
	}

	contexts := make(map[manifest.Source]resolvedResourceContext, len(resources))
	buildOptions := manifest.BuildOptions{
		Identity: options.Identity,
		ExternalContextFor: func(resource manifest.Document, resolution manifest.DiscoveryResolution) (*contract.ExternalContext, error) {
			external, namespaceStatus, diagnostics, err := resolveNamespace(ctx, resource, resolution, configurations, options)
			if err != nil {
				return nil, err
			}
			contexts[resource.Source] = resolvedResourceContext{namespace: namespaceStatus, diagnostics: diagnostics}
			return external, nil
		},
	}
	built, err := manifest.BuildScenarios(resources, configurations, resolver, buildOptions)
	if err != nil {
		return Result{}, err
	}
	result.BuiltScenarios = built
	for index := range result.Resources {
		resolved, ok := contexts[result.Resources[index].Resource]
		if !ok {
			return Result{}, &contract.InternalError{Operation: "associate resource context", Err: errors.New("builder did not request resource context")}
		}
		result.Resources[index].Completeness.Namespace = resolved.namespace
		result.Resources[index].Diagnostics = appendDiagnostics(result.Resources[index].Diagnostics, resolved.diagnostics)
		if err := result.Resources[index].Completeness.Validate(); err != nil {
			return Result{}, &contract.InternalError{Operation: "validate adapter completeness", Err: err}
		}
	}
	return result, nil
}

type resolvedResourceContext struct {
	namespace   manifest.ContextStatus
	diagnostics []manifest.Diagnostic
}

func resolveDiscovery(selected *Hydration) (manifest.ProfileMatch, manifest.Resolver, manifest.ContextStatus, []manifest.Diagnostic, bool, error) {
	if selected == nil {
		profile := manifest.ProfileMatch{Status: manifest.ProfileMatchDeclared, Profile: contract.Kubernetes136DefaultProfile()}
		return profile,
			manifest.OfflineResolver{},
			manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: kube136.ProfileID},
			nil,
			true,
			nil
	}
	if selected.Reader == nil || selected.SourceLabel == "" {
		return manifest.ProfileMatch{}, nil, manifest.ContextStatus{}, nil, false, &contract.InvalidInputError{Field: "hydration", Err: errors.New("verified reader and logical source label are required")}
	}
	if err := selected.ProfileMatch.Validate(); err != nil ||
		selected.ProfileMatch.Status != manifest.ProfileMatchVerified ||
		selected.ProfileMatch.ObservedKubernetesVersion != kube136.KubernetesVersion {
		if err == nil {
			err = fmt.Errorf("observed Kubernetes version %q is not %s", selected.ProfileMatch.ObservedKubernetesVersion, kube136.KubernetesVersion)
		}
		return manifest.ProfileMatch{}, nil, manifest.ContextStatus{}, nil, false, &contract.UnsupportedCapabilityError{Capability: "verified Kubernetes hydration profile", Err: err}
	}
	discovery := selected.Reader.Discover()
	if discovery.Status == hydration.ReadStatusSuccess {
		resolver, err := discovery.Resolver(selected.SourceLabel)
		if err != nil {
			return manifest.ProfileMatch{}, nil, manifest.ContextStatus{}, nil, false, &contract.InternalError{Operation: "build verified discovery resolver", Err: err}
		}
		return selected.ProfileMatch,
			resolver,
			manifest.ContextStatus{Status: manifest.CompletenessHydrated, SourceLabel: selected.SourceLabel},
			nil,
			true,
			nil
	}
	status := manifest.CompletenessUnsupported
	if discovery.Status == hydration.ReadStatusForbidden {
		status = manifest.CompletenessForbidden
	}
	diagnostic := incompleteDiagnostic(manifest.Source{}, "Kubernetes discovery is unavailable; use an exact 1.36.2 context or a supported built-in GVK")
	return selected.ProfileMatch, nil, manifest.ContextStatus{Status: status}, []manifest.Diagnostic{diagnostic}, false, nil
}

func resolveConfigurations(ctx context.Context, options Options, discoveryReady bool) ([]manifest.ConfigurationInput, manifest.ContextStatus, []manifest.Diagnostic) {
	if options.FileConfigurations != nil {
		if len(options.FileConfigurations) == 0 {
			return nil, manifest.ContextStatus{Status: manifest.CompletenessMissing}, []manifest.Diagnostic{
				incompleteDiagnostic(manifest.Source{}, "explicit webhook configuration input contains no supported configurations"),
			}
		}
		return append([]manifest.ConfigurationInput(nil), options.FileConfigurations...),
			manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: fileConfigurationSourceLabel},
			nil
	}
	if options.Hydration == nil {
		return nil, manifest.ContextStatus{Status: manifest.CompletenessMissing}, []manifest.Diagnostic{
			incompleteDiagnostic(manifest.Source{}, "webhook configuration is missing; provide --webhook-config or --context"),
		}
	}
	if !discoveryReady {
		return nil, manifest.ContextStatus{Status: manifest.CompletenessMissing}, []manifest.Diagnostic{
			incompleteDiagnostic(manifest.Source{}, "webhook configurations were not read because discovery did not complete"),
		}
	}
	validating := options.Hydration.Reader.ListValidatingConfigurations(ctx)
	mutating := options.Hydration.Reader.ListMutatingConfigurations(ctx)
	configurations := make([]manifest.ConfigurationInput, 0, len(validating.Configurations)+len(mutating.Configurations))
	for index := range validating.Configurations {
		configuration := validating.Configurations[index].DeepCopy()
		configurations = append(configurations, manifest.ConfigurationInput{
			Source:     clusterConfigurationSource(options.Hydration.SourceLabel, "ValidatingWebhookConfiguration", configuration.Name),
			Validating: configuration,
		})
	}
	for index := range mutating.Configurations {
		configuration := mutating.Configurations[index].DeepCopy()
		configurations = append(configurations, manifest.ConfigurationInput{
			Source:   clusterConfigurationSource(options.Hydration.SourceLabel, "MutatingWebhookConfiguration", configuration.Name),
			Mutating: configuration,
		})
	}
	status := combinedConfigurationStatus(validating.Status, mutating.Status, len(configurations))
	if status.Status == manifest.CompletenessHydrated {
		status.SourceLabel = options.Hydration.SourceLabel
		return configurations, status, nil
	}
	message := "cluster webhook configurations are unavailable; provide --webhook-config"
	return configurations, status, []manifest.Diagnostic{incompleteDiagnostic(manifest.Source{}, message)}
}

func combinedConfigurationStatus(validating, mutating hydration.ReadStatus, count int) manifest.ContextStatus {
	if validating == hydration.ReadStatusForbidden || mutating == hydration.ReadStatusForbidden {
		return manifest.ContextStatus{Status: manifest.CompletenessForbidden}
	}
	if validating == hydration.ReadStatusMalformed || mutating == hydration.ReadStatusMalformed {
		return manifest.ContextStatus{Status: manifest.CompletenessUnsupported}
	}
	if count > 0 && readSucceeded(validating) && readSucceeded(mutating) {
		return manifest.ContextStatus{Status: manifest.CompletenessHydrated}
	}
	return manifest.ContextStatus{Status: manifest.CompletenessMissing}
}

func readSucceeded(status hydration.ReadStatus) bool {
	return status == hydration.ReadStatusSuccess || status == hydration.ReadStatusEmpty
}

func resolveNamespace(
	ctx context.Context,
	resource manifest.Document,
	resolution manifest.DiscoveryResolution,
	configurations []manifest.ConfigurationInput,
	options Options,
) (*contract.ExternalContext, manifest.ContextStatus, []manifest.Diagnostic, error) {
	required := resolution.Namespaced && configurationsHaveNamespaceSelector(configurations)
	if options.FileNamespace != nil {
		if options.FileNamespace.Class != manifest.DocumentClassNamespace || options.FileNamespace.Namespace == nil {
			return nil, manifest.ContextStatus{}, nil, &contract.InvalidInputError{Field: "namespace-object", Err: errors.New("explicit namespace input must be one core/v1 Namespace")}
		}
		if err := options.FileNamespace.Source.Validate(); err != nil {
			return nil, manifest.ContextStatus{}, nil, &contract.InvalidInputError{Field: "namespace-object.source", Err: err}
		}
		if !required {
			return cloneBaseExternal(options.ExternalContext, nil),
				manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: options.FileNamespace.Source.Label},
				nil,
				nil
		}
		return cloneBaseExternal(options.ExternalContext, options.FileNamespace.Namespace),
			manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: options.FileNamespace.Source.Label},
			nil,
			nil
	}
	if !required {
		return cloneBaseExternal(options.ExternalContext, nil), manifest.ContextStatus{Status: manifest.CompletenessNotRequired}, nil, nil
	}
	if options.Hydration == nil || resource.Object == nil || resource.Object.GetNamespace() == "" {
		return cloneBaseExternal(options.ExternalContext, nil),
			manifest.ContextStatus{Status: manifest.CompletenessMissing},
			[]manifest.Diagnostic{incompleteDiagnostic(resource.Source, "Namespace selector context is missing; provide --namespace-object or an explicit --context")},
			nil
	}
	read := options.Hydration.Reader.GetNamespace(ctx, resource.Object.GetNamespace())
	if read.Status == hydration.ReadStatusSuccess && read.Namespace != nil {
		return cloneBaseExternal(options.ExternalContext, read.Namespace),
			manifest.ContextStatus{Status: manifest.CompletenessHydrated, SourceLabel: options.Hydration.SourceLabel},
			nil,
			nil
	}
	status := manifest.CompletenessMissing
	if read.Status == hydration.ReadStatusForbidden {
		status = manifest.CompletenessForbidden
	} else if read.Status == hydration.ReadStatusMalformed {
		status = manifest.CompletenessUnsupported
	}
	return cloneBaseExternal(options.ExternalContext, nil),
		manifest.ContextStatus{Status: status},
		[]manifest.Diagnostic{incompleteDiagnostic(resource.Source, "Namespace selector context is unavailable; provide --namespace-object")},
		nil
}

func configurationsHaveNamespaceSelector(configurations []manifest.ConfigurationInput) bool {
	for _, configuration := range configurations {
		if configuration.Validating != nil {
			for _, webhook := range configuration.Validating.Webhooks {
				if webhook.NamespaceSelector != nil {
					return true
				}
			}
		}
		if configuration.Mutating != nil {
			for _, webhook := range configuration.Mutating.Webhooks {
				if webhook.NamespaceSelector != nil {
					return true
				}
			}
		}
	}
	return false
}

func cloneBaseExternal(base *contract.ExternalContext, namespace *corev1.Namespace) *contract.ExternalContext {
	if base == nil && namespace == nil {
		return nil
	}
	result := contract.ExternalContext{}
	if base != nil {
		result = *base
	}
	if namespace != nil {
		result.Namespace = namespace.DeepCopy()
	} else {
		result.Namespace = nil
	}
	return &result
}

func clusterConfigurationSource(contextLabel, kind, name string) manifest.Source {
	return manifest.Source{Kind: manifest.SourceKindCluster, Label: fmt.Sprintf("%s/%s/%s", contextLabel, kind, name)}
}

func optionalContextStatus(provided bool, sourceLabel string) manifest.ContextStatus {
	if provided {
		return manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: sourceLabel}
	}
	return manifest.ContextStatus{Status: manifest.CompletenessNotRequired}
}

func logicalSource(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func incompleteDiagnostic(source manifest.Source, message string) manifest.Diagnostic {
	return manifest.Diagnostic{
		Code:          manifest.DiagnosticCodeIncomplete,
		Severity:      contract.DiagnosticSeverityWarning,
		Message:       message,
		SourceLabel:   source.Label,
		DocumentIndex: source.DocumentIndex,
	}
}

func appendDiagnostics(target []manifest.Diagnostic, groups ...[]manifest.Diagnostic) []manifest.Diagnostic {
	for _, group := range groups {
		for _, diagnostic := range group {
			copied := diagnostic
			target = append(target, copied)
		}
	}
	return target
}

func diagnosticsForResource(source manifest.Source, groups ...[]manifest.Diagnostic) []manifest.Diagnostic {
	result := appendDiagnostics(nil, groups...)
	for index := range result {
		if result[index].SourceLabel == "" {
			result[index].SourceLabel = source.Label
			result[index].DocumentIndex = source.DocumentIndex
		}
	}
	return result
}

// FinalizeCompleteness applies only context gaps that the existing evaluator
// actually reached. This keeps unused optional fixtures not-required while
// preserving fail-closed identity, equivalence, authorization, and Namespace
// outcomes.
func FinalizeCompleteness(base manifest.ContextCompleteness, evaluations []manifest.AdapterEvaluation) manifest.ContextCompleteness {
	for _, evaluation := range evaluations {
		for _, webhook := range evaluation.Result.Webhooks {
			for _, diagnostic := range webhook.Diagnostics {
				switch diagnostic.Code {
				case contract.ReasonCodeNamespaceContextMissing:
					base.Namespace = missingUnlessUnavailable(base.Namespace)
				case contract.ReasonCodeIdentityContextMissing:
					base.Identity = manifest.ContextStatus{Status: manifest.CompletenessMissing}
				case contract.ReasonCodeEquivalenceContextMissing:
					base.Equivalence = manifest.ContextStatus{Status: manifest.CompletenessMissing}
				case contract.ReasonCodeAuthorizationContextMissing:
					base.Authorization = manifest.ContextStatus{Status: manifest.CompletenessMissing}
				}
			}
		}
	}
	return base
}

func missingUnlessUnavailable(current manifest.ContextStatus) manifest.ContextStatus {
	if current.Status == manifest.CompletenessForbidden || current.Status == manifest.CompletenessUnsupported {
		return current
	}
	return manifest.ContextStatus{Status: manifest.CompletenessMissing}
}
