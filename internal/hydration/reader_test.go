package hydration

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/manifest"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func TestReadStatusVocabulary(t *testing.T) {
	t.Parallel()

	for _, status := range []ReadStatus{
		ReadStatusSuccess,
		ReadStatusEmpty,
		ReadStatusForbidden,
		ReadStatusNotFound,
		ReadStatusUnavailable,
		ReadStatusMalformed,
	} {
		if !status.IsValid() {
			t.Errorf("ReadStatus(%q).IsValid() = false", status)
		}
	}
	if ReadStatus("unknown").IsValid() {
		t.Error("ReadStatus(unknown).IsValid() = true")
	}
}

func TestNewReaderRequiresVerifiedSession(t *testing.T) {
	t.Parallel()

	if _, err := (*Session)(nil).NewReader(); err == nil {
		t.Fatal("nil Session.NewReader() error = nil")
	}
	session := &Session{
		profileMatch: manifest.ProfileMatch{Status: manifest.ProfileMatchMismatch},
		restConfig:   &rest.Config{Host: "https://cluster.invalid"},
		httpClient:   &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("must not transmit") })},
	}
	_, err := session.NewReader()
	if !errors.Is(err, contract.ErrUnsupportedCapability) {
		t.Fatalf("unverified Session.NewReader() error = %v, want unsupported capability", err)
	}
}

func TestReaderDependenciesStayWithinApprovedReadOnlyBoundary(t *testing.T) {
	t.Parallel()

	parsed, err := parser.ParseFile(token.NewFileSet(), "reader.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse reader.go imports: %v", err)
	}
	allowed := map[string]bool{
		"context": true,
		"errors":  true,
		"fmt":     true,
		"sort":    true,
		"strings": true,
		"github.com/silbaram/admitrace/internal/compat/kube136":      true,
		"github.com/silbaram/admitrace/internal/contract":            true,
		"github.com/silbaram/admitrace/internal/manifest":            true,
		"github.com/silbaram/admitrace/internal/resourcecatalog":     true,
		"k8s.io/api/admissionregistration/v1":                        true,
		"k8s.io/api/core/v1":                                         true,
		"k8s.io/apimachinery/pkg/api/errors":                         true,
		"k8s.io/apimachinery/pkg/apis/meta/v1":                       true,
		"k8s.io/client-go/discovery":                                 true,
		"k8s.io/client-go/kubernetes/typed/admissionregistration/v1": true,
		"k8s.io/client-go/kubernetes/typed/core/v1":                  true,
	}
	for _, imported := range parsed.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %s: %v", imported.Path.Value, err)
		}
		if !allowed[path] {
			t.Errorf("reader.go imports unapproved dependency %q", path)
		}
	}
}

func TestVerifiedReaderUsesOnlyApprovedGETEndpoints(t *testing.T) {
	t.Parallel()

	responses := map[string]string{
		"/api/v1/namespaces/team-a": `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"team-a"}}`,
		"/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": `{"apiVersion":"admissionregistration.k8s.io/v1","kind":"ValidatingWebhookConfigurationList","items":[{"metadata":{"name":"zeta"}},{"metadata":{"name":"alpha"}}]}`,
		"/apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations":   `{"apiVersion":"admissionregistration.k8s.io/v1","kind":"MutatingWebhookConfigurationList","items":[{"metadata":{"name":"mutator"}}]}`,
	}
	requested := make([]string, 0, len(responses))
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet {
			t.Fatalf("request method = %s, want GET", request.Method)
		}
		requested = append(requested, request.URL.Path)
		body, ok := responses[request.URL.Path]
		if !ok {
			t.Fatalf("unexpected Kubernetes API path %q", request.URL.Path)
		}
		return jsonHTTPResponse(http.StatusOK, body), nil
	})
	guarded, err := NewGETOnlyRoundTripper(base)
	if err != nil {
		t.Fatalf("NewGETOnlyRoundTripper() error = %v", err)
	}
	session := &Session{
		contextLabel: "context:explicit",
		profileMatch: manifest.ProfileMatch{
			Status:                    manifest.ProfileMatchVerified,
			Profile:                   contract.Kubernetes136DefaultProfile(),
			ObservedKubernetesVersion: "1.36.2",
		},
		restConfig: &rest.Config{Host: "https://cluster.invalid", Transport: guarded},
		httpClient: &http.Client{Transport: guarded},
	}
	reader, err := session.NewReader()
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if got := reader.GetNamespace(context.Background(), "team-a"); got.Status != ReadStatusSuccess || got.Namespace.Name != "team-a" {
		t.Fatalf("GetNamespace() = %#v, want team-a success", got)
	}
	validating := reader.ListValidatingConfigurations(context.Background())
	if validating.Status != ReadStatusSuccess || len(validating.Configurations) != 2 || validating.Configurations[0].Name != "alpha" || validating.Configurations[1].Name != "zeta" {
		t.Fatalf("ListValidatingConfigurations() = %#v, want deterministic success", validating)
	}
	mutating := reader.ListMutatingConfigurations(context.Background())
	if mutating.Status != ReadStatusSuccess || len(mutating.Configurations) != 1 || mutating.Configurations[0].Name != "mutator" {
		t.Fatalf("ListMutatingConfigurations() = %#v, want deterministic success", mutating)
	}
	want := []string{
		"/api/v1/namespaces/team-a",
		"/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations",
		"/apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations",
	}
	if strings.Join(requested, "\n") != strings.Join(want, "\n") {
		t.Errorf("requested paths = %q, want %q", requested, want)
	}
}

func TestDiscoverBuildsExactGVKMappingsIncludingCRDs(t *testing.T) {
	t.Parallel()

	reader := &Reader{discovery: discoveryStub{lists: []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: metav1.Verbs{"get", "create"}},
				{Name: "pods/status", Kind: "Pod", Namespaced: true, Verbs: metav1.Verbs{"get", "update"}},
				{Name: "componentstatuses", Kind: "ComponentStatus", Verbs: metav1.Verbs{"get"}},
			},
		},
		{
			GroupVersion: "example.io/v1alpha1",
			APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: metav1.Verbs{"create"}}},
		},
	}}}
	result := reader.Discover()
	if result.Status != ReadStatusSuccess || len(result.Resources) != 2 {
		t.Fatalf("Discover() = %#v, want two exact resources", result)
	}
	resolver, err := result.Resolver("context:explicit")
	if err != nil {
		t.Fatalf("Resolver() error = %v", err)
	}
	resolved, err := resolver.Resolve(schema.GroupVersionKind{Group: "example.io", Version: "v1alpha1", Kind: "Widget"})
	if err != nil {
		t.Fatalf("Resolve(Widget) error = %v", err)
	}
	if resolved.GVR != (schema.GroupVersionResource{Group: "example.io", Version: "v1alpha1", Resource: "widgets"}) || !resolved.Namespaced {
		t.Errorf("Widget resolution = %#v, want exact widgets mapping", resolved)
	}
	_, err = resolver.Resolve(schema.GroupVersionKind{Group: "example.io", Version: "v1alpha1", Kind: "Widgets"})
	if !errors.Is(err, contract.ErrUnsupportedCapability) {
		t.Errorf("heuristic plural resolution error = %v, want unsupported", err)
	}
}

func TestDiscoverDistinguishesEmptyMalformedAndUnavailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stub   discoveryStub
		status ReadStatus
	}{
		{name: "empty", stub: discoveryStub{lists: []*metav1.APIResourceList{
			{GroupVersion: "v1"},
		}}, status: ReadStatusEmpty},
		{name: "nil list", stub: discoveryStub{lists: []*metav1.APIResourceList{nil}}, status: ReadStatusMalformed},
		{name: "missing group version", stub: discoveryStub{lists: []*metav1.APIResourceList{
			{},
		}}, status: ReadStatusMalformed},
		{name: "bad group version", stub: discoveryStub{lists: []*metav1.APIResourceList{
			{GroupVersion: "bad/value/extra", APIResources: []metav1.APIResource{
				{Name: "things", Kind: "Thing", Verbs: metav1.Verbs{"create"}},
			}},
		}}, status: ReadStatusMalformed},
		{name: "missing kind", stub: discoveryStub{lists: []*metav1.APIResourceList{
			{GroupVersion: "v1", APIResources: []metav1.APIResource{
				{Name: "things", Verbs: metav1.Verbs{"create"}},
			}},
		}}, status: ReadStatusMalformed},
		{name: "duplicate GVK", stub: discoveryStub{lists: []*metav1.APIResourceList{
			{GroupVersion: "v1", APIResources: []metav1.APIResource{
				{Name: "things", Kind: "Thing", Verbs: metav1.Verbs{"create"}},
				{Name: "otherthings", Kind: "Thing", Verbs: metav1.Verbs{"create"}},
			}},
		}}, status: ReadStatusMalformed},
		{name: "forbidden", stub: discoveryStub{err: apierrors.NewForbidden(schema.GroupResource{Resource: "apis"}, "", errors.New("denied"))}, status: ReadStatusForbidden},
		{name: "not found", stub: discoveryStub{err: apierrors.NewNotFound(schema.GroupResource{Resource: "apis"}, "")}, status: ReadStatusNotFound},
		{name: "unavailable", stub: discoveryStub{err: errors.New("network unavailable")}, status: ReadStatusUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := (&Reader{discovery: test.stub}).Discover()
			if result.Status != test.status {
				t.Errorf("Discover() status = %q, want %q (error %v)", result.Status, test.status, result.Err)
			}
		})
	}
}

func TestResourceReadersReturnStableCompletenessStatuses(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		err    error
		status ReadStatus
	}{
		{name: "forbidden", err: apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "team-a", errors.New("denied")), status: ReadStatusForbidden},
		{name: "not found", err: apierrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, "team-a"), status: ReadStatusNotFound},
		{name: "unavailable", err: errors.New("connection refused"), status: ReadStatusUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := (&Reader{namespaces: namespaceStub{err: test.err}}).GetNamespace(context.Background(), "team-a")
			if result.Status != test.status {
				t.Errorf("GetNamespace() status = %q, want %q", result.Status, test.status)
			}
		})
	}
	if result := (&Reader{namespaces: namespaceStub{namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}}}).GetNamespace(context.Background(), "team-a"); result.Status != ReadStatusMalformed {
		t.Errorf("mismatched Namespace status = %q, want malformed", result.Status)
	}
	if result := (&Reader{validatingConfigs: validatingStub{list: &admissionregistrationv1.ValidatingWebhookConfigurationList{}}}).ListValidatingConfigurations(context.Background()); result.Status != ReadStatusEmpty {
		t.Errorf("empty validating LIST status = %q, want empty", result.Status)
	}
	if result := (&Reader{mutatingConfigs: mutatingStub{list: nil}}).ListMutatingConfigurations(context.Background()); result.Status != ReadStatusMalformed {
		t.Errorf("nil mutating LIST status = %q, want malformed", result.Status)
	}
	if result := (&Reader{mutatingConfigs: mutatingStub{err: apierrors.NewForbidden(schema.GroupResource{Resource: "mutatingwebhookconfigurations"}, "", errors.New("denied"))}}).ListMutatingConfigurations(context.Background()); result.Status != ReadStatusForbidden {
		t.Errorf("forbidden mutating LIST status = %q, want forbidden", result.Status)
	}
}

type discoveryStub struct {
	lists []*metav1.APIResourceList
	err   error
}

func (stub discoveryStub) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, stub.lists, stub.err
}

type namespaceStub struct {
	namespace *corev1.Namespace
	err       error
}

func (stub namespaceStub) Get(context.Context, string, metav1.GetOptions) (*corev1.Namespace, error) {
	return stub.namespace, stub.err
}

type validatingStub struct {
	list *admissionregistrationv1.ValidatingWebhookConfigurationList
	err  error
}

func (stub validatingStub) List(context.Context, metav1.ListOptions) (*admissionregistrationv1.ValidatingWebhookConfigurationList, error) {
	return stub.list, stub.err
}

type mutatingStub struct {
	list *admissionregistrationv1.MutatingWebhookConfigurationList
	err  error
}

func (stub mutatingStub) List(context.Context, metav1.ListOptions) (*admissionregistrationv1.MutatingWebhookConfigurationList, error) {
	return stub.list, stub.err
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
