package contract

import (
	"encoding/json"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Scenario is a self-contained admission webhook routing evaluation fixture.
type Scenario struct {
	APIVersion           string               `json:"apiVersion"`
	Kind                 string               `json:"kind"`
	Metadata             ScenarioMetadata     `json:"metadata"`
	CompatibilityProfile CompatibilityProfile `json:"compatibilityProfile"`
	Configuration        WebhookConfiguration `json:"configuration"`
	Request              AdmissionRequest     `json:"request"`
	ExternalContext      *ExternalContext     `json:"externalContext,omitempty"`
	Expectations         []WebhookExpectation `json:"expectations,omitempty"`
}

// ScenarioMetadata contains the stable identity of a Scenario.
type ScenarioMetadata struct {
	Name string `json:"name"`
}

// ConfigurationKind identifies the supported admissionregistration configuration kinds.
type ConfigurationKind string

const (
	// ConfigurationKindValidating identifies a ValidatingWebhookConfiguration.
	ConfigurationKindValidating ConfigurationKind = "ValidatingWebhookConfiguration"
	// ConfigurationKindMutating identifies a MutatingWebhookConfiguration.
	ConfigurationKindMutating ConfigurationKind = "MutatingWebhookConfiguration"
)

// WebhookConfiguration is a one-of wrapper for the supported Kubernetes configuration types.
type WebhookConfiguration struct {
	Validating *kube136.ValidatingWebhookConfiguration `json:"validatingWebhookConfiguration,omitempty"`
	Mutating   *kube136.MutatingWebhookConfiguration   `json:"mutatingWebhookConfiguration,omitempty"`
}

// Kind returns the selected configuration kind and whether exactly one configuration is present.
func (configuration WebhookConfiguration) Kind() (ConfigurationKind, bool) {
	switch {
	case configuration.Validating != nil && configuration.Mutating == nil:
		return ConfigurationKindValidating, true
	case configuration.Validating == nil && configuration.Mutating != nil:
		return ConfigurationKindMutating, true
	default:
		return "", false
	}
}

// HasExactlyOne reports whether exactly one supported configuration is present.
func (configuration WebhookConfiguration) HasExactlyOne() bool {
	_, ok := configuration.Kind()
	return ok
}

// RequestScope identifies whether the requested resource is cluster-scoped or namespaced.
type RequestScope string

const (
	// RequestScopeCluster identifies a cluster-scoped request resource.
	RequestScopeCluster RequestScope = "Cluster"
	// RequestScopeNamespaced identifies a namespaced request resource.
	RequestScopeNamespaced RequestScope = "Namespaced"
)

// AdmissionRequest represents the request snapshot evaluated by a Scenario.
//
// Object, OldObject, and Options remain raw JSON so fields belonging to user
// resources are not constrained by the Scenario contract. A nil RawMessage is
// absent while a RawMessage containing "null" is an explicit null payload.
type AdmissionRequest struct {
	UID                types.UID                    `json:"uid,omitempty"`
	Kind               metav1.GroupVersionKind      `json:"kind"`
	Resource           metav1.GroupVersionResource  `json:"resource"`
	SubResource        string                       `json:"subResource,omitempty"`
	RequestKind        *metav1.GroupVersionKind     `json:"requestKind,omitempty"`
	RequestResource    *metav1.GroupVersionResource `json:"requestResource,omitempty"`
	RequestSubResource string                       `json:"requestSubResource,omitempty"`
	Name               string                       `json:"name,omitempty"`
	Namespace          string                       `json:"namespace,omitempty"`
	Operation          admissionv1.Operation        `json:"operation"`
	Scope              RequestScope                 `json:"scope"`
	UserInfo           authenticationv1.UserInfo    `json:"userInfo"`
	Object             json.RawMessage              `json:"object,omitempty"`
	OldObject          json.RawMessage              `json:"oldObject,omitempty"`
	DryRun             *bool                        `json:"dryRun,omitempty"`
	Options            json.RawMessage              `json:"options,omitempty"`
}

// ExternalContext contains evaluation inputs that cannot be derived from the request snapshot.
type ExternalContext struct {
	Namespace     *corev1.Namespace       `json:"namespace,omitempty"`
	Authorization []AuthorizationDecision `json:"authorization,omitempty"`
	Equivalence   []EquivalenceMapping    `json:"equivalence,omitempty"`
}

// AuthorizationVerdict is the result returned by an authorization fixture.
type AuthorizationVerdict string

const (
	// AuthorizationVerdictAllow represents an allowed authorization query.
	AuthorizationVerdictAllow AuthorizationVerdict = "allow"
	// AuthorizationVerdictDeny represents a denied authorization query.
	AuthorizationVerdictDeny AuthorizationVerdict = "deny"
	// AuthorizationVerdictNoOpinion represents an explicit no-opinion decision.
	AuthorizationVerdictNoOpinion AuthorizationVerdict = "no-opinion"
	// AuthorizationVerdictError represents an authorization evaluation error.
	AuthorizationVerdictError AuthorizationVerdict = "error"
)

// AuthorizationDecision associates one normalized query with its fixture verdict.
type AuthorizationDecision struct {
	Query   AuthorizationQuery   `json:"query"`
	Verdict AuthorizationVerdict `json:"verdict"`
	Reason  string               `json:"reason,omitempty"`
}

// AuthorizationQuery is a one-of wrapper for resource and non-resource authorization queries.
type AuthorizationQuery struct {
	Resource    *ResourceAuthorizationQuery    `json:"resource,omitempty"`
	NonResource *NonResourceAuthorizationQuery `json:"nonResource,omitempty"`
}

// HasExactlyOne reports whether exactly one authorization query form is present.
func (query AuthorizationQuery) HasExactlyOne() bool {
	return (query.Resource == nil) != (query.NonResource == nil)
}

// ResourceAuthorizationQuery is a normalized Kubernetes resource authorization query.
type ResourceAuthorizationQuery struct {
	Verb        string `json:"verb"`
	APIGroup    string `json:"apiGroup,omitempty"`
	APIVersion  string `json:"apiVersion,omitempty"`
	Resource    string `json:"resource"`
	Subresource string `json:"subresource,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name,omitempty"`
}

// NonResourceAuthorizationQuery is a normalized Kubernetes non-resource authorization query.
type NonResourceAuthorizationQuery struct {
	Verb string `json:"verb"`
	Path string `json:"path"`
}

// EquivalenceMapping associates a request resource with ordered equivalent resources.
type EquivalenceMapping struct {
	Request     ResourceReference    `json:"request"`
	Equivalents []EquivalentResource `json:"equivalents"`
}

// ResourceReference identifies the request GVR and subresource used for an equivalence lookup.
type ResourceReference struct {
	GVR         metav1.GroupVersionResource `json:"gvr"`
	Subresource string                      `json:"subresource,omitempty"`
}

// EquivalentResource identifies one ordered equivalent GVR, GVK, and subresource.
type EquivalentResource struct {
	GVR         metav1.GroupVersionResource `json:"gvr"`
	GVK         metav1.GroupVersionKind     `json:"gvk"`
	Subresource string                      `json:"subresource,omitempty"`
}

// WebhookExpectation declares the expected result for one named webhook.
type WebhookExpectation struct {
	WebhookName        string        `json:"webhookName"`
	Determination      Determination `json:"determination"`
	Outcome            *Outcome      `json:"outcome,omitempty"`
	TerminalReasonCode ReasonCode    `json:"terminalReasonCode,omitempty"`
}

// ValidateOutcome permits a determinate expectation to omit the optional outcome assertion.
func (expectation WebhookExpectation) ValidateOutcome() error {
	if !expectation.Determination.IsValid() {
		return &ValidationError{Field: "determination", Value: string(expectation.Determination), Err: ErrInvalidEnumValue}
	}
	if expectation.Outcome == nil {
		return nil
	}
	if expectation.Determination != DeterminationDeterminate {
		return &ValidationError{Field: "outcome", Value: string(*expectation.Outcome), Err: ErrOutcomeRequiresDeterminate}
	}
	if !expectation.Outcome.IsValid() {
		return &ValidationError{Field: "outcome", Value: string(*expectation.Outcome), Err: ErrInvalidEnumValue}
	}
	return nil
}

// Validate checks expectation vocabulary while preserving optional assertions.
func (expectation WebhookExpectation) Validate() error {
	if err := expectation.ValidateOutcome(); err != nil {
		return err
	}
	if expectation.TerminalReasonCode != "" && !expectation.TerminalReasonCode.IsRegistered() {
		return &ValidationError{Field: "terminalReasonCode", Value: string(expectation.TerminalReasonCode), Err: ErrUnregisteredReasonCode}
	}
	return nil
}
