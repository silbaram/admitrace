package fixture

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/silbaram/admitrace/internal/contract"
	kubeauthorizer "k8s.io/apiserver/pkg/authorization/authorizer"
)

var (
	// ErrDuplicateAuthorizationQuery identifies more than one fixture entry for
	// the same canonical query with identical results.
	ErrDuplicateAuthorizationQuery = errors.New("duplicate authorization query")
	// ErrConflictingAuthorizationDecision identifies fixture entries that share
	// a canonical query but disagree on verdict or reason.
	ErrConflictingAuthorizationDecision = errors.New("conflicting authorization decision for query")

	errAuthorizationFixtureMissing    = errors.New("authorization fixture decision is required")
	errAuthorizationFixtureEvaluation = errors.New("authorization fixture returned an error")
	errAuthorizationSelectorQuery     = errors.New("authorization selectors are not represented by the fixture contract")
)

type authorizationQueryKind uint8

const (
	authorizationQueryResource authorizationQueryKind = iota + 1
	authorizationQueryNonResource
)

// authorizationKey is a collision-safe canonical query key. Resource keys use
// verb, API group, API version, resource, subresource, namespace, and name;
// non-resource keys use verb and path. Every field is matched exactly and is
// case-sensitive.
type authorizationKey struct {
	kind        authorizationQueryKind
	verb        string
	apiGroup    string
	apiVersion  string
	resource    string
	subresource string
	namespace   string
	name        string
	path        string
}

type authorizationEntry struct {
	verdict contract.AuthorizationVerdict
	reason  string
}

// Authorizer replays immutable authorization decisions from an in-memory
// fixture without contacting Kubernetes or any external service.
type Authorizer struct {
	decisions map[authorizationKey]authorizationEntry
}

var _ kubeauthorizer.Authorizer = Authorizer{}

// NewAuthorizer validates and owns the supplied decisions. Each resource or
// non-resource canonical key must occur exactly once.
func NewAuthorizer(decisions []contract.AuthorizationDecision) (Authorizer, error) {
	authorizer := Authorizer{
		decisions: make(map[authorizationKey]authorizationEntry, len(decisions)),
	}
	for i, decision := range decisions {
		decisionPath := fmt.Sprintf(".externalContext.authorization[%d]", i)
		key, err := authorizationKeyForQuery(decision.Query, decisionPath+".query")
		if err != nil {
			return Authorizer{}, err
		}
		if !validAuthorizationVerdict(decision.Verdict) {
			return Authorizer{}, invalidAuthorization(
				decisionPath+".verdict",
				fmt.Errorf("unknown authorization verdict %q", decision.Verdict),
			)
		}

		entry := authorizationEntry{verdict: decision.Verdict, reason: decision.Reason}
		if existing, found := authorizer.decisions[key]; found {
			cause := ErrDuplicateAuthorizationQuery
			if existing != entry {
				cause = ErrConflictingAuthorizationDecision
			}
			return Authorizer{}, invalidAuthorization(decisionPath+".query", cause)
		}
		authorizer.decisions[key] = entry
	}
	return authorizer, nil
}

// Authorize implements the Kubernetes authorizer contract using an exact
// fixture lookup. A missing decision is contract.ErrMissingContext, while an
// explicit error verdict is contract.ErrKubernetesEvaluation.
func (a Authorizer) Authorize(_ context.Context, attributes kubeauthorizer.Attributes) (kubeauthorizer.Decision, string, error) {
	key, err := authorizationKeyForAttributes(attributes)
	if err != nil {
		return kubeauthorizer.DecisionNoOpinion, "", err
	}
	entry, found := a.decisions[key]
	if !found {
		return kubeauthorizer.DecisionNoOpinion, "", &contract.MissingContextError{
			Context:   "authorization decision",
			Reference: authorizationReference(key),
			Err:       errAuthorizationFixtureMissing,
		}
	}

	switch entry.verdict {
	case contract.AuthorizationVerdictAllow:
		return kubeauthorizer.DecisionAllow, entry.reason, nil
	case contract.AuthorizationVerdictDeny:
		return kubeauthorizer.DecisionDeny, entry.reason, nil
	case contract.AuthorizationVerdictNoOpinion:
		return kubeauthorizer.DecisionNoOpinion, entry.reason, nil
	case contract.AuthorizationVerdictError:
		return kubeauthorizer.DecisionNoOpinion, entry.reason, &contract.KubernetesEvaluationError{
			Operation: "evaluate authorization fixture",
			Err:       errAuthorizationFixtureEvaluation,
		}
	default:
		return kubeauthorizer.DecisionNoOpinion, "", &contract.InternalError{
			Operation: "replay authorization fixture",
			Err:       fmt.Errorf("unvalidated authorization verdict %q", entry.verdict),
		}
	}
}

func authorizationKeyForAttributes(attributes kubeauthorizer.Attributes) (authorizationKey, error) {
	if nilAuthorizationAttributes(attributes) {
		return authorizationKey{}, invalidAuthorization("authorization attributes", errors.New("attributes must not be nil"))
	}
	if err := rejectAuthorizationSelectors(attributes); err != nil {
		return authorizationKey{}, err
	}

	query := contract.AuthorizationQuery{}
	if attributes.IsResourceRequest() {
		query.Resource = &contract.ResourceAuthorizationQuery{
			Verb:        attributes.GetVerb(),
			APIGroup:    attributes.GetAPIGroup(),
			APIVersion:  attributes.GetAPIVersion(),
			Resource:    attributes.GetResource(),
			Subresource: attributes.GetSubresource(),
			Namespace:   attributes.GetNamespace(),
			Name:        attributes.GetName(),
		}
	} else {
		query.NonResource = &contract.NonResourceAuthorizationQuery{
			Verb: attributes.GetVerb(),
			Path: attributes.GetPath(),
		}
	}
	return authorizationKeyForQuery(query, "authorization attributes")
}

func nilAuthorizationAttributes(attributes kubeauthorizer.Attributes) bool {
	if attributes == nil {
		return true
	}
	value := reflect.ValueOf(attributes)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func rejectAuthorizationSelectors(attributes kubeauthorizer.Attributes) error {
	fieldRequirements, fieldErr := attributes.GetFieldSelector()
	labelRequirements, labelErr := attributes.GetLabelSelector()
	if fieldErr == nil && labelErr == nil && len(fieldRequirements) == 0 && len(labelRequirements) == 0 {
		return nil
	}
	return &contract.UnsupportedCapabilityError{
		Capability: "authorization queries with field or label selectors",
		Err:        authorizationSelectorCause(fieldErr, labelErr),
	}
}

func authorizationSelectorCause(fieldErr, labelErr error) error {
	causes := []error{errAuthorizationSelectorQuery}
	if fieldErr != nil {
		causes = append(causes, fmt.Errorf("field selector: %w", fieldErr))
	}
	if labelErr != nil {
		causes = append(causes, fmt.Errorf("label selector: %w", labelErr))
	}
	return errors.Join(causes...)
}

func authorizationKeyForQuery(query contract.AuthorizationQuery, path string) (authorizationKey, error) {
	if !query.HasExactlyOne() {
		return authorizationKey{}, invalidAuthorization(path, errors.New("exactly one of resource and nonResource must be set"))
	}
	if query.Resource != nil {
		if err := validateResourceAuthorizationQuery(*query.Resource, path+".resource"); err != nil {
			return authorizationKey{}, err
		}
		return authorizationKey{
			kind:        authorizationQueryResource,
			verb:        query.Resource.Verb,
			apiGroup:    query.Resource.APIGroup,
			apiVersion:  query.Resource.APIVersion,
			resource:    query.Resource.Resource,
			subresource: query.Resource.Subresource,
			namespace:   query.Resource.Namespace,
			name:        query.Resource.Name,
		}, nil
	}

	if err := validateNonResourceAuthorizationQuery(*query.NonResource, path+".nonResource"); err != nil {
		return authorizationKey{}, err
	}
	return authorizationKey{
		kind: authorizationQueryNonResource,
		verb: query.NonResource.Verb,
		path: query.NonResource.Path,
	}, nil
}

func validateResourceAuthorizationQuery(query contract.ResourceAuthorizationQuery, path string) error {
	fields := []struct {
		name     string
		value    string
		required bool
	}{
		{name: "verb", value: query.Verb, required: true},
		{name: "apiGroup", value: query.APIGroup},
		{name: "apiVersion", value: query.APIVersion},
		{name: "resource", value: query.Resource, required: true},
		{name: "subresource", value: query.Subresource},
		{name: "namespace", value: query.Namespace},
		{name: "name", value: query.Name},
	}
	for _, field := range fields {
		if err := validateAuthorizationToken(field.value, field.required, path+"."+field.name); err != nil {
			return err
		}
	}
	return nil
}

func validateNonResourceAuthorizationQuery(query contract.NonResourceAuthorizationQuery, path string) error {
	if err := validateAuthorizationToken(query.Verb, true, path+".verb"); err != nil {
		return err
	}
	if strings.TrimSpace(query.Path) == "" {
		return invalidAuthorization(path+".path", errors.New("value must not be empty"))
	}
	if strings.IndexFunc(query.Path, unicode.IsSpace) >= 0 {
		return invalidAuthorization(path+".path", errors.New("value must not contain whitespace"))
	}
	return nil
}

func validateAuthorizationToken(value string, required bool, path string) error {
	if required && value == "" {
		return invalidAuthorization(path, errors.New("value must not be empty"))
	}
	if strings.ContainsRune(value, '/') {
		return invalidAuthorization(path, errors.New("value must not contain a slash"))
	}
	if strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return invalidAuthorization(path, errors.New("value must not contain whitespace"))
	}
	return nil
}

func validAuthorizationVerdict(verdict contract.AuthorizationVerdict) bool {
	switch verdict {
	case contract.AuthorizationVerdictAllow,
		contract.AuthorizationVerdictDeny,
		contract.AuthorizationVerdictNoOpinion,
		contract.AuthorizationVerdictError:
		return true
	default:
		return false
	}
}

func authorizationReference(key authorizationKey) string {
	if key.kind == authorizationQueryNonResource {
		return "non-resource request"
	}
	return fmt.Sprintf("resource %s/%s/%s/%s", key.apiGroup, key.apiVersion, key.resource, key.subresource)
}

func invalidAuthorization(field string, err error) error {
	return &contract.InvalidInputError{Field: field, Err: err}
}
