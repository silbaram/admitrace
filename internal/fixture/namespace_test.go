package fixture_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	"github.com/silbaram/admitrace/internal/normalize"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNamespaceProviderLookupUsesExactOwnedFixtures(t *testing.T) {
	t.Parallel()

	original := testNamespace("team-a", "fixture")
	provider := mustNamespaceProvider(t, fixture.NamespaceEntry{Name: "team-a", Namespace: original})

	original.Labels["source"] = "mutated-input"
	original.Annotations["owner"] = "mutated-input"
	original.Finalizers[0] = "mutated-input"

	first, err := provider.Lookup("team-a")
	if err != nil {
		t.Fatalf("Lookup(team-a) error = %v", err)
	}
	assertNamespaceMetadata(t, first, "fixture", "platform", "example.io/protect")

	first.Labels["source"] = "mutated-result"
	first.Annotations["owner"] = "mutated-result"
	first.Finalizers[0] = "mutated-result"

	second, err := provider.Lookup("team-a")
	if err != nil {
		t.Fatalf("second Lookup(team-a) error = %v", err)
	}
	assertNamespaceMetadata(t, second, "fixture", "platform", "example.io/protect")
	if got := original.Labels["source"]; got != "mutated-input" {
		t.Errorf("original label = %q, want independent value mutated-input", got)
	}

	for _, name := range []string{"Team-a", "team-a ", " team-a"} {
		name := name
		t.Run("exact miss "+name, func(t *testing.T) {
			t.Parallel()

			got, err := provider.Lookup(name)
			if got != nil {
				t.Errorf("Lookup(%q) namespace = %#v, want nil", name, got)
			}
			if !errors.Is(err, contract.ErrMissingContext) {
				t.Errorf("Lookup(%q) error = %v, want ErrMissingContext", name, err)
			}
		})
	}
}

func TestNewNamespaceProviderRejectsAmbiguousEntries(t *testing.T) {
	t.Parallel()

	cause := errors.New("recorded failure")
	tests := []struct {
		name      string
		entries   []fixture.NamespaceEntry
		wantField string
	}{
		{
			name:      "blank key",
			entries:   []fixture.NamespaceEntry{{Namespace: testNamespace("team-a", "fixture")}},
			wantField: "namespaceEntries[0]",
		},
		{
			name:      "missing object and error",
			entries:   []fixture.NamespaceEntry{{Name: "team-a"}},
			wantField: "namespaceEntries[0]",
		},
		{
			name:      "object and error both set",
			entries:   []fixture.NamespaceEntry{{Name: "team-a", Namespace: testNamespace("team-a", "fixture"), Err: cause}},
			wantField: "namespaceEntries[0]",
		},
		{
			name:      "object name mismatch",
			entries:   []fixture.NamespaceEntry{{Name: "team-a", Namespace: testNamespace("team-b", "fixture")}},
			wantField: "namespaceEntries[0]",
		},
		{
			name: "duplicate key",
			entries: []fixture.NamespaceEntry{
				{Name: "team-a", Namespace: testNamespace("team-a", "first")},
				{Name: "team-a", Err: cause},
			},
			wantField: "namespaceEntries[1]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := fixture.NewNamespaceProvider(test.entries...)
			if !errors.Is(err, contract.ErrInvalidInput) {
				t.Fatalf("NewNamespaceProvider() error = %v, want ErrInvalidInput", err)
			}
			var invalid *contract.InvalidInputError
			if !errors.As(err, &invalid) {
				t.Fatalf("NewNamespaceProvider() error type = %T, want *contract.InvalidInputError", err)
			}
			if invalid.Field != test.wantField {
				t.Errorf("InvalidInputError.Field = %q, want %q", invalid.Field, test.wantField)
			}
		})
	}
}

func TestNamespaceProviderDistinguishesMissingAndLookupError(t *testing.T) {
	t.Parallel()

	cause := errors.New("recorded fixture failure")
	provider := mustNamespaceProvider(t, fixture.NamespaceEntry{Name: "faulty", Err: cause})

	_, missingErr := provider.Lookup("missing")
	if !errors.Is(missingErr, contract.ErrMissingContext) {
		t.Errorf("missing error = %v, want ErrMissingContext", missingErr)
	}
	if errors.Is(missingErr, contract.ErrKubernetesEvaluation) {
		t.Errorf("missing error = %v, must not match ErrKubernetesEvaluation", missingErr)
	}
	var missing *contract.MissingContextError
	if !errors.As(missingErr, &missing) {
		t.Fatalf("missing error type = %T, want *contract.MissingContextError", missingErr)
	}
	if missing.Context != "namespace" || missing.Reference != "missing" {
		t.Errorf("missing error = %#v, want namespace reference missing", missing)
	}

	_, lookupErr := provider.Lookup("faulty")
	if !errors.Is(lookupErr, contract.ErrKubernetesEvaluation) {
		t.Errorf("lookup error = %v, want ErrKubernetesEvaluation", lookupErr)
	}
	if errors.Is(lookupErr, contract.ErrMissingContext) {
		t.Errorf("lookup error = %v, must not match ErrMissingContext", lookupErr)
	}
	if !errors.Is(lookupErr, cause) {
		t.Errorf("lookup error = %v, want recorded cause", lookupErr)
	}
	var typed *fixture.NamespaceLookupError
	if !errors.As(fmt.Errorf("resolve selector context: %w", lookupErr), &typed) {
		t.Fatalf("wrapped lookup error type = %T, want *fixture.NamespaceLookupError", lookupErr)
	}
	if typed.Namespace != "faulty" {
		t.Errorf("NamespaceLookupError.Namespace = %q, want faulty", typed.Namespace)
	}
}

func TestNamespaceProviderContextForKubernetesBranches(t *testing.T) {
	t.Parallel()

	provider := mustNamespaceProvider(t,
		fixture.NamespaceEntry{Name: "team-a", Namespace: testNamespace("team-a", "fixture")},
		fixture.NamespaceEntry{Name: "faulty", Err: errors.New("must not be read")},
	)
	object := namespaceSnapshot("team-a", "request-object")
	oldObject := namespaceSnapshot("team-a", "old-object")
	tests := []struct {
		name          string
		request       normalize.RequestContext
		wantRequired  bool
		wantSource    fixture.NamespaceContextSource
		wantLabel     string
		wantNamespace bool
	}{
		{
			name: "Namespace create uses request object",
			request: normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
				Scope:     contract.RequestScopeCluster,
				Object:    object,
			},
			wantRequired:  true,
			wantSource:    fixture.NamespaceContextRequestObject,
			wantLabel:     "request-object",
			wantNamespace: true,
		},
		{
			name: "Namespace update uses request object",
			request: normalize.RequestContext{
				Operation: admissionv1.Update,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
				Scope:     contract.RequestScopeCluster,
				Namespace: "team-a",
				Object:    object,
			},
			wantRequired:  true,
			wantSource:    fixture.NamespaceContextRequestObject,
			wantLabel:     "request-object",
			wantNamespace: true,
		},
		{
			name: "Namespace delete uses request name fixture rather than old object",
			request: normalize.RequestContext{
				Operation: admissionv1.Delete,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
				Scope:     contract.RequestScopeCluster,
				Name:      "team-a",
				OldObject: oldObject,
			},
			wantRequired:  true,
			wantSource:    fixture.NamespaceContextFixture,
			wantLabel:     "fixture",
			wantNamespace: true,
		},
		{
			name: "Namespace subresource uses fixture",
			request: normalize.RequestContext{
				Operation:   admissionv1.Update,
				Resource:    schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
				Subresource: "finalize",
				Scope:       contract.RequestScopeCluster,
				Name:        "team-a",
				Object:      object,
			},
			wantRequired:  true,
			wantSource:    fixture.NamespaceContextFixture,
			wantLabel:     "fixture",
			wantNamespace: true,
		},
		{
			name: "namespaced resource uses exact fixture",
			request: normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "pods"},
				Scope:     contract.RequestScopeNamespaced,
				Namespace: "team-a",
			},
			wantRequired:  true,
			wantSource:    fixture.NamespaceContextFixture,
			wantLabel:     "fixture",
			wantNamespace: true,
		},
		{
			name: "cluster scoped non-Namespace bypasses lookup",
			request: normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "nodes"},
				Scope:     contract.RequestScopeCluster,
				Namespace: "faulty",
			},
			wantRequired:  false,
			wantSource:    fixture.NamespaceContextNotRequired,
			wantNamespace: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := provider.ContextFor(test.request)
			if err != nil {
				t.Fatalf("ContextFor() error = %v", err)
			}
			if got.Required != test.wantRequired {
				t.Errorf("Required = %t, want %t", got.Required, test.wantRequired)
			}
			if got.Source != test.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, test.wantSource)
			}
			if (got.Namespace != nil) != test.wantNamespace {
				t.Fatalf("Namespace presence = %t, want %t", got.Namespace != nil, test.wantNamespace)
			}
			if got.Namespace != nil && got.Namespace.Labels["source"] != test.wantLabel {
				t.Errorf("Namespace label = %q, want %q", got.Namespace.Labels["source"], test.wantLabel)
			}
		})
	}
}

func TestNamespaceProviderContextForPreservesRequiredContextErrors(t *testing.T) {
	t.Parallel()

	cause := errors.New("recorded fixture failure")
	tests := []struct {
		name        string
		provider    fixture.NamespaceProvider
		namespace   string
		want        error
		doesNotWant error
		checkType   func(error) bool
	}{
		{
			name:        "missing fixture",
			provider:    mustNamespaceProvider(t),
			namespace:   "missing",
			want:        contract.ErrMissingContext,
			doesNotWant: contract.ErrKubernetesEvaluation,
			checkType: func(err error) bool {
				var target *contract.MissingContextError
				return errors.As(err, &target) && target.Reference == "missing"
			},
		},
		{
			name:        "explicit lookup error",
			provider:    mustNamespaceProvider(t, fixture.NamespaceEntry{Name: "faulty", Err: cause}),
			namespace:   "faulty",
			want:        contract.ErrKubernetesEvaluation,
			doesNotWant: contract.ErrMissingContext,
			checkType: func(err error) bool {
				var target *fixture.NamespaceLookupError
				return errors.As(err, &target) && target.Namespace == "faulty" && errors.Is(err, cause)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := test.provider.ContextFor(normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "pods"},
				Scope:     contract.RequestScopeNamespaced,
				Namespace: test.namespace,
			})
			if !errors.Is(err, test.want) {
				t.Errorf("ContextFor() error = %v, want category %v", err, test.want)
			}
			if errors.Is(err, test.doesNotWant) {
				t.Errorf("ContextFor() error = %v, must not match category %v", err, test.doesNotWant)
			}
			if !test.checkType(err) {
				t.Errorf("ContextFor() error = %v, want preserved typed detail", err)
			}
		})
	}
}

func TestNamespaceProviderRequestObjectContextIsOwned(t *testing.T) {
	t.Parallel()

	provider := mustNamespaceProvider(t)
	raw := json.RawMessage(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"team-a","labels":{"source":"request-object"},"annotations":{"owner":"platform"},"finalizers":["example.io/protect"]}}`)
	request := normalize.RequestContext{
		Operation: admissionv1.Create,
		Resource:  schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
		Scope:     contract.RequestScopeCluster,
		Object: normalize.ObjectSnapshot{
			State:       normalize.ObjectSnapshotObject,
			Raw:         raw,
			Name:        "team-a",
			Labels:      map[string]string{"source": "request-object"},
			Annotations: map[string]string{"owner": "platform"},
		},
	}

	first, err := provider.ContextFor(request)
	if err != nil {
		t.Fatalf("ContextFor() error = %v", err)
	}
	first.Namespace.Labels["source"] = "mutated-result"
	first.Namespace.Annotations["owner"] = "mutated-result"
	first.Namespace.Finalizers[0] = "mutated-result"
	if got := request.Object.Labels["source"]; got != "request-object" {
		t.Errorf("request object label = %q, want independent value request-object", got)
	}
	if !bytes.Contains(request.Object.Raw, []byte(`"source":"request-object"`)) {
		t.Errorf("request object raw was modified: %s", request.Object.Raw)
	}

	second, err := provider.ContextFor(request)
	if err != nil {
		t.Fatalf("second ContextFor() error = %v", err)
	}
	assertNamespaceMetadata(t, second.Namespace, "request-object", "platform", "example.io/protect")
}

func TestNamespaceProviderRequestObjectMustBePresent(t *testing.T) {
	t.Parallel()

	provider := mustNamespaceProvider(t)
	for _, state := range []normalize.ObjectSnapshotState{normalize.ObjectSnapshotAbsent, normalize.ObjectSnapshotNull} {
		state := state
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()

			_, err := provider.ContextFor(normalize.RequestContext{
				Operation: admissionv1.Create,
				Resource:  schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
				Scope:     contract.RequestScopeCluster,
				Object:    normalize.ObjectSnapshot{State: state},
			})
			if !errors.Is(err, contract.ErrKubernetesEvaluation) {
				t.Errorf("ContextFor() error = %v, want ErrKubernetesEvaluation", err)
			}
			if errors.Is(err, contract.ErrMissingContext) {
				t.Errorf("ContextFor() error = %v, must not match ErrMissingContext", err)
			}
		})
	}
}

func mustNamespaceProvider(t *testing.T, entries ...fixture.NamespaceEntry) fixture.NamespaceProvider {
	t.Helper()

	provider, err := fixture.NewNamespaceProvider(entries...)
	if err != nil {
		t.Fatalf("NewNamespaceProvider() error = %v", err)
	}
	return provider
}

func testNamespace(name, source string) *corev1.Namespace {
	return &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      map[string]string{"source": source},
			Annotations: map[string]string{"owner": "platform"},
			Finalizers:  []string{"example.io/protect"},
		},
	}
}

func namespaceSnapshot(name, source string) normalize.ObjectSnapshot {
	raw := json.RawMessage(fmt.Sprintf(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":%q,"labels":{"source":%q},"annotations":{"owner":"platform"},"finalizers":["example.io/protect"]}}`, name, source))
	return normalize.ObjectSnapshot{
		State:       normalize.ObjectSnapshotObject,
		Raw:         raw,
		APIVersion:  "v1",
		Kind:        "Namespace",
		GVK:         schema.GroupVersionKind{Version: "v1", Kind: "Namespace"},
		Name:        name,
		Labels:      map[string]string{"source": source},
		Annotations: map[string]string{"owner": "platform"},
	}
}

func assertNamespaceMetadata(t *testing.T, got *corev1.Namespace, wantLabel, wantAnnotation, wantFinalizer string) {
	t.Helper()

	if got == nil {
		t.Fatal("namespace = nil, want object")
	}
	if got.Labels["source"] != wantLabel {
		t.Errorf("label = %q, want %q", got.Labels["source"], wantLabel)
	}
	if got.Annotations["owner"] != wantAnnotation {
		t.Errorf("annotation = %q, want %q", got.Annotations["owner"], wantAnnotation)
	}
	if !reflect.DeepEqual(got.Finalizers, []string{wantFinalizer}) {
		t.Errorf("finalizers = %#v, want %#v", got.Finalizers, []string{wantFinalizer})
	}
}
