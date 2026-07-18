package hydration

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/resourcecatalog"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	discoveryclient "k8s.io/client-go/discovery"
	admissionclient "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
)

// ReadStatus is the stable completeness-facing outcome of one allowed API read.
type ReadStatus string

const (
	// ReadStatusSuccess identifies a non-empty successful response.
	ReadStatusSuccess ReadStatus = "success"
	// ReadStatusEmpty identifies a successful LIST or discovery with no eligible items.
	ReadStatusEmpty ReadStatus = "empty"
	// ReadStatusForbidden identifies an authorization denial.
	ReadStatusForbidden ReadStatus = "forbidden"
	// ReadStatusNotFound identifies an absent requested endpoint or object.
	ReadStatusNotFound ReadStatus = "not-found"
	// ReadStatusUnavailable identifies transport, timeout, or server availability failure.
	ReadStatusUnavailable ReadStatus = "unavailable"
	// ReadStatusMalformed identifies a response that cannot form a safe exact snapshot.
	ReadStatusMalformed ReadStatus = "malformed"
)

// IsValid reports whether status belongs to the reader result vocabulary.
func (status ReadStatus) IsValid() bool {
	switch status {
	case ReadStatusSuccess, ReadStatusEmpty, ReadStatusForbidden, ReadStatusNotFound, ReadStatusUnavailable, ReadStatusMalformed:
		return true
	default:
		return false
	}
}

// DiscoveryResult contains exact CREATE-capable discovery mappings.
type DiscoveryResult struct {
	Status    ReadStatus                 `json:"status"`
	Resources []resourcecatalog.Resource `json:"resources,omitempty"`
	Err       error                      `json:"-"`
}

// Resolver constructs a verified exact resolver only from successful discovery.
func (result DiscoveryResult) Resolver(sourceLabel string) (*manifest.VerifiedDiscoveryResolver, error) {
	if result.Status != ReadStatusSuccess {
		return nil, &contract.InvalidInputError{Field: "discovery", Err: fmt.Errorf("discovery status %q cannot resolve resources", result.Status)}
	}
	return manifest.NewVerifiedDiscoveryResolver(sourceLabel, result.Resources)
}

// NamespaceResult contains one exact Namespace GET outcome.
type NamespaceResult struct {
	Status    ReadStatus        `json:"status"`
	Namespace *corev1.Namespace `json:"namespace,omitempty"`
	Err       error             `json:"-"`
}

// ValidatingConfigurationsResult contains one cluster-scoped LIST outcome.
type ValidatingConfigurationsResult struct {
	Status         ReadStatus                                               `json:"status"`
	Configurations []admissionregistrationv1.ValidatingWebhookConfiguration `json:"configurations,omitempty"`
	Err            error                                                    `json:"-"`
}

// MutatingConfigurationsResult contains one cluster-scoped LIST outcome.
type MutatingConfigurationsResult struct {
	Status         ReadStatus                                             `json:"status"`
	Configurations []admissionregistrationv1.MutatingWebhookConfiguration `json:"configurations,omitempty"`
	Err            error                                                  `json:"-"`
}

type discoveryAPI interface {
	ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error)
}

type namespaceAPI interface {
	Get(context.Context, string, metav1.GetOptions) (*corev1.Namespace, error)
}

type validatingConfigurationsAPI interface {
	List(context.Context, metav1.ListOptions) (*admissionregistrationv1.ValidatingWebhookConfigurationList, error)
}

type mutatingConfigurationsAPI interface {
	List(context.Context, metav1.ListOptions) (*admissionregistrationv1.MutatingWebhookConfigurationList, error)
}

// Reader exposes only the approved discovery, Namespace GET, and configuration
// LIST operations.
type Reader struct {
	discovery         discoveryAPI
	namespaces        namespaceAPI
	validatingConfigs validatingConfigurationsAPI
	mutatingConfigs   mutatingConfigurationsAPI
}

// NewReader creates typed readers only from an exact-profile verified Session.
func (session *Session) NewReader() (*Reader, error) {
	if session == nil || session.restConfig == nil || session.httpClient == nil {
		return nil, &Error{Kind: ErrorKindClientConfig, Operation: "require verified hydration session", Err: &contract.InvalidInputError{Field: "session", Err: errors.New("verified Session is required")}}
	}
	if session.profileMatch.Status != manifest.ProfileMatchVerified {
		return nil, &Error{Kind: ErrorKindProfileMismatch, Operation: "require exact verified profile before readers", ObservedVersion: session.profileMatch.ObservedKubernetesVersion, ProfileMatch: &session.profileMatch, Err: &contract.UnsupportedCapabilityError{Capability: "Kubernetes compatibility profile"}}
	}
	discovery, err := discoveryclient.NewDiscoveryClientForConfigAndClient(session.restConfig, session.httpClient)
	if err != nil {
		return nil, &Error{Kind: ErrorKindClientConfig, Operation: "create discovery reader", Err: err}
	}
	core, err := coreclient.NewForConfigAndClient(session.restConfig, session.httpClient)
	if err != nil {
		return nil, &Error{Kind: ErrorKindClientConfig, Operation: "create Namespace reader", Err: err}
	}
	admission, err := admissionclient.NewForConfigAndClient(session.restConfig, session.httpClient)
	if err != nil {
		return nil, &Error{Kind: ErrorKindClientConfig, Operation: "create WebhookConfiguration readers", Err: err}
	}
	return &Reader{
		discovery:         discovery,
		namespaces:        core.Namespaces(),
		validatingConfigs: admission.ValidatingWebhookConfigurations(),
		mutatingConfigs:   admission.MutatingWebhookConfigurations(),
	}, nil
}

// Discover reads all served API group versions and returns exact creatable
// root-resource mappings, including CRDs served by the verified context.
func (reader *Reader) Discover() DiscoveryResult {
	if reader == nil || reader.discovery == nil {
		return DiscoveryResult{Status: ReadStatusMalformed, Err: errors.New("discovery reader is required")}
	}
	_, lists, err := reader.discovery.ServerGroupsAndResources()
	if err != nil {
		return DiscoveryResult{Status: classifyReadError(err), Err: err}
	}
	if err := validateDiscoveryLists(lists); err != nil {
		return DiscoveryResult{Status: ReadStatusMalformed, Err: err}
	}
	if countCreatableResources(lists) == 0 {
		return DiscoveryResult{Status: ReadStatusEmpty, Resources: []resourcecatalog.Resource{}}
	}
	catalog, err := resourcecatalog.Generate(kube136.ProfileID, kube136.KubernetesVersion, lists, nil)
	if err != nil {
		return DiscoveryResult{Status: ReadStatusMalformed, Err: err}
	}
	return DiscoveryResult{Status: ReadStatusSuccess, Resources: catalog.Resources}
}

// GetNamespace performs one exact Namespace GET.
func (reader *Reader) GetNamespace(ctx context.Context, name string) NamespaceResult {
	if reader == nil || reader.namespaces == nil || ctx == nil || name == "" {
		return NamespaceResult{Status: ReadStatusMalformed, Err: errors.New("Namespace reader, context, and name are required")}
	}
	namespace, err := reader.namespaces.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return NamespaceResult{Status: classifyReadError(err), Err: err}
	}
	if namespace == nil || namespace.Name != name {
		return NamespaceResult{Status: ReadStatusMalformed, Err: errors.New("Namespace response identity does not match request")}
	}
	return NamespaceResult{Status: ReadStatusSuccess, Namespace: namespace.DeepCopy()}
}

// ListValidatingConfigurations performs one cluster-scoped LIST and returns
// deterministic metadata-name order.
func (reader *Reader) ListValidatingConfigurations(ctx context.Context) ValidatingConfigurationsResult {
	if reader == nil || reader.validatingConfigs == nil || ctx == nil {
		return ValidatingConfigurationsResult{Status: ReadStatusMalformed, Err: errors.New("validating configuration reader and context are required")}
	}
	list, err := reader.validatingConfigs.List(ctx, metav1.ListOptions{})
	if err != nil {
		return ValidatingConfigurationsResult{Status: classifyReadError(err), Err: err}
	}
	if list == nil {
		return ValidatingConfigurationsResult{Status: ReadStatusMalformed, Err: errors.New("validating configuration LIST returned nil")}
	}
	if len(list.Items) == 0 {
		return ValidatingConfigurationsResult{Status: ReadStatusEmpty, Configurations: []admissionregistrationv1.ValidatingWebhookConfiguration{}}
	}
	configurations := append([]admissionregistrationv1.ValidatingWebhookConfiguration(nil), list.Items...)
	sort.SliceStable(configurations, func(i, j int) bool { return configurations[i].Name < configurations[j].Name })
	for index := range configurations {
		configurations[index] = *configurations[index].DeepCopy()
	}
	return ValidatingConfigurationsResult{Status: ReadStatusSuccess, Configurations: configurations}
}

// ListMutatingConfigurations performs one cluster-scoped LIST and returns
// deterministic metadata-name order.
func (reader *Reader) ListMutatingConfigurations(ctx context.Context) MutatingConfigurationsResult {
	if reader == nil || reader.mutatingConfigs == nil || ctx == nil {
		return MutatingConfigurationsResult{Status: ReadStatusMalformed, Err: errors.New("mutating configuration reader and context are required")}
	}
	list, err := reader.mutatingConfigs.List(ctx, metav1.ListOptions{})
	if err != nil {
		return MutatingConfigurationsResult{Status: classifyReadError(err), Err: err}
	}
	if list == nil {
		return MutatingConfigurationsResult{Status: ReadStatusMalformed, Err: errors.New("mutating configuration LIST returned nil")}
	}
	if len(list.Items) == 0 {
		return MutatingConfigurationsResult{Status: ReadStatusEmpty, Configurations: []admissionregistrationv1.MutatingWebhookConfiguration{}}
	}
	configurations := append([]admissionregistrationv1.MutatingWebhookConfiguration(nil), list.Items...)
	sort.SliceStable(configurations, func(i, j int) bool { return configurations[i].Name < configurations[j].Name })
	for index := range configurations {
		configurations[index] = *configurations[index].DeepCopy()
	}
	return MutatingConfigurationsResult{Status: ReadStatusSuccess, Configurations: configurations}
}

func classifyReadError(err error) ReadStatus {
	switch {
	case apierrors.IsForbidden(err):
		return ReadStatusForbidden
	case apierrors.IsNotFound(err):
		return ReadStatusNotFound
	default:
		return ReadStatusUnavailable
	}
}

func validateDiscoveryLists(lists []*metav1.APIResourceList) error {
	for listIndex, list := range lists {
		if list == nil || strings.TrimSpace(list.GroupVersion) == "" {
			return fmt.Errorf("discovery list %d has no groupVersion", listIndex)
		}
		for resourceIndex, resource := range list.APIResources {
			if !containsVerb(resource.Verbs, "create") || strings.Contains(resource.Name, "/") {
				continue
			}
			if resource.Name == "" || resource.Kind == "" {
				return fmt.Errorf("discovery resource %d in %s has no name or kind", resourceIndex, list.GroupVersion)
			}
		}
	}
	return nil
}

func countCreatableResources(lists []*metav1.APIResourceList) int {
	count := 0
	for _, list := range lists {
		if list == nil {
			continue
		}
		for _, resource := range list.APIResources {
			if containsVerb(resource.Verbs, "create") && !strings.Contains(resource.Name, "/") && resource.Name != "" && resource.Kind != "" {
				count++
			}
		}
	}
	return count
}

func containsVerb(verbs metav1.Verbs, want string) bool {
	for _, verb := range verbs {
		if verb == want {
			return true
		}
	}
	return false
}
