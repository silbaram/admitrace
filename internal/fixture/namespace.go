package fixture

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/normalize"
	corev1 "k8s.io/api/core/v1"
)

var errNamespaceFixtureMissing = errors.New("namespace fixture is required")

// NamespaceEntry associates one exact namespace key with either an object or
// an explicit lookup error. Exactly one of Namespace and Err must be set.
type NamespaceEntry struct {
	Name      string
	Namespace *corev1.Namespace
	Err       error
}

// NamespaceLookupError reports an explicit error stored for a namespace
// fixture lookup. It is a Kubernetes evaluation error rather than missing
// fixture context.
type NamespaceLookupError struct {
	Namespace string
	Err       error
}

// Error returns the lookup category, namespace, and cause description.
func (e *NamespaceLookupError) Error() string {
	if e == nil {
		return contract.ErrKubernetesEvaluation.Error()
	}
	message := contract.ErrKubernetesEvaluation.Error() + ": namespace fixture lookup"
	if e.Namespace != "" {
		message += fmt.Sprintf(" %q", e.Namespace)
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

// Unwrap exposes the explicit fixture lookup cause.
func (e *NamespaceLookupError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is classifies NamespaceLookupError as a Kubernetes evaluation failure.
func (e *NamespaceLookupError) Is(target error) bool {
	return target == contract.ErrKubernetesEvaluation
}

type namespaceEntry struct {
	namespace *corev1.Namespace
	err       error
}

// NamespaceProvider resolves namespace selector context exclusively from
// immutable in-memory fixture entries and request snapshots.
type NamespaceProvider struct {
	entries map[string]namespaceEntry
}

// NewNamespaceProvider validates and owns a copy of every fixture entry.
// Entry names are exact, case-sensitive lookup keys.
func NewNamespaceProvider(entries ...NamespaceEntry) (NamespaceProvider, error) {
	provider := NamespaceProvider{entries: make(map[string]namespaceEntry, len(entries))}
	for i, entry := range entries {
		field := fmt.Sprintf("namespaceEntries[%d]", i)
		if entry.Name == "" {
			return NamespaceProvider{}, invalidNamespaceEntry(field, errors.New("namespace key must not be empty"))
		}
		if (entry.Namespace == nil) == (entry.Err == nil) {
			return NamespaceProvider{}, invalidNamespaceEntry(field, errors.New("exactly one of namespace and error must be set"))
		}
		if entry.Namespace != nil && entry.Namespace.Name != entry.Name {
			return NamespaceProvider{}, invalidNamespaceEntry(field, fmt.Errorf("namespace object name %q does not match key %q", entry.Namespace.Name, entry.Name))
		}
		if _, exists := provider.entries[entry.Name]; exists {
			return NamespaceProvider{}, invalidNamespaceEntry(field, fmt.Errorf("duplicate namespace key %q", entry.Name))
		}
		stored := namespaceEntry{err: entry.Err}
		if entry.Namespace != nil {
			stored.namespace = entry.Namespace.DeepCopy()
		}
		provider.entries[entry.Name] = stored
	}
	return provider, nil
}

// Lookup returns a caller-owned copy of the fixture matching name exactly.
// A missing entry is returned as contract.ErrMissingContext, while an explicit
// entry error is returned as contract.ErrKubernetesEvaluation.
func (p NamespaceProvider) Lookup(name string) (*corev1.Namespace, error) {
	entry, ok := p.entries[name]
	if !ok {
		return nil, &contract.MissingContextError{
			Context:   "namespace",
			Reference: name,
			Err:       errNamespaceFixtureMissing,
		}
	}
	if entry.err != nil {
		return nil, &NamespaceLookupError{Namespace: name, Err: entry.err}
	}
	return entry.namespace.DeepCopy(), nil
}

// NamespaceContextSource identifies where selector namespace metadata came from.
type NamespaceContextSource string

const (
	// NamespaceContextNotRequired identifies a cluster-scoped, non-Namespace request.
	NamespaceContextNotRequired NamespaceContextSource = "not-required"
	// NamespaceContextRequestObject identifies Namespace metadata read from request.object.
	NamespaceContextRequestObject NamespaceContextSource = "request-object"
	// NamespaceContextFixture identifies Namespace metadata read by exact fixture lookup.
	NamespaceContextFixture NamespaceContextSource = "fixture"
)

// NamespaceContext contains caller-owned namespace metadata for selector
// evaluation. Required is false only for cluster-scoped, non-Namespace requests.
type NamespaceContext struct {
	Required  bool
	Source    NamespaceContextSource
	Namespace *corev1.Namespace
}

// ContextFor resolves the namespace selector context required by request.
func (p NamespaceProvider) ContextFor(request normalize.RequestContext) (NamespaceContext, error) {
	mode := kube136.NamespaceContextModeFor(
		request.Operation,
		request.Resource,
		request.Subresource,
		request.Scope == contract.RequestScopeNamespaced,
	)
	switch mode {
	case kube136.NamespaceContextModeRequestObject:
		namespace, err := namespaceFromRequestObject(request.Object)
		if err != nil {
			return NamespaceContext{}, err
		}
		return NamespaceContext{
			Required:  true,
			Source:    NamespaceContextRequestObject,
			Namespace: namespace,
		}, nil
	case kube136.NamespaceContextModeNotRequired:
		return NamespaceContext{Source: NamespaceContextNotRequired}, nil
	case kube136.NamespaceContextModeFixture:
		namespace, err := p.Lookup(request.Namespace)
		if err != nil {
			return NamespaceContext{}, err
		}
		return NamespaceContext{
			Required:  true,
			Source:    NamespaceContextFixture,
			Namespace: namespace,
		}, nil
	default:
		return NamespaceContext{}, &contract.InternalError{
			Operation: "resolve namespace context",
			Err:       fmt.Errorf("unsupported namespace context mode %q", mode),
		}
	}
}

func namespaceFromRequestObject(snapshot normalize.ObjectSnapshot) (*corev1.Namespace, error) {
	if snapshot.State != normalize.ObjectSnapshotObject {
		return nil, &contract.KubernetesEvaluationError{
			Operation: "read namespace request object",
			Err:       fmt.Errorf("request object state is %q", snapshot.State),
		}
	}
	var namespace corev1.Namespace
	if err := json.Unmarshal(snapshot.Raw, &namespace); err != nil {
		return nil, &contract.KubernetesEvaluationError{
			Operation: "decode namespace request object",
			Err:       err,
		}
	}
	return namespace.DeepCopy(), nil
}

func invalidNamespaceEntry(field string, err error) error {
	return &contract.InvalidInputError{Field: field, Err: err}
}
