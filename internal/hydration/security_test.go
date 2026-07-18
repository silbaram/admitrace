package hydration

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"k8s.io/client-go/rest"
)

func TestVerifiedHydrationHTTPAuditAllowsOnlyExactGETSurface(t *testing.T) {
	t.Parallel()

	transport := &auditRoundTripper{responses: hydrationAuditResponses()}
	factory := Factory{
		loadConfig: func(Options) (*rest.Config, error) {
			return &rest.Config{Host: "https://cluster.invalid"}, nil
		},
		buildTransport: func(*rest.Config) (http.RoundTripper, error) {
			return transport, nil
		},
	}
	session, err := factory.Connect(context.Background(), Options{Context: "security-audit"})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	reader, err := session.NewReader()
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	if result := reader.Discover(); result.Status != ReadStatusSuccess {
		t.Fatalf("Discover() = %#v, want success", result)
	}
	if result := reader.GetNamespace(context.Background(), "team-a"); result.Status != ReadStatusSuccess {
		t.Fatalf("GetNamespace() = %#v, want success", result)
	}
	if result := reader.ListValidatingConfigurations(context.Background()); result.Status != ReadStatusEmpty {
		t.Fatalf("ListValidatingConfigurations() = %#v, want empty", result)
	}
	if result := reader.ListMutatingConfigurations(context.Background()); result.Status != ReadStatusEmpty {
		t.Fatalf("ListMutatingConfigurations() = %#v, want empty", result)
	}

	wantPaths := []string{
		"/api",
		"/api/v1",
		"/apis",
		"/apis/admissionregistration.k8s.io/v1",
		"/apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations",
		"/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations",
		"/api/v1/namespaces/team-a",
		"/version",
	}
	gotPaths := transport.paths()
	sort.Strings(wantPaths)
	if strings.Join(gotPaths, "\n") != strings.Join(wantPaths, "\n") {
		t.Errorf("observed hydration paths = %q, want exact allowlist %q", gotPaths, wantPaths)
	}
	for _, request := range transport.requests {
		if request.Method != http.MethodGet {
			t.Errorf("observed method = %q, want GET", request.Method)
		}
		for _, forbidden := range []string{"watch", "dryRun"} {
			if request.URL.Query().Has(forbidden) {
				t.Errorf("GET %s unexpectedly used %s query", request.URL.Path, forbidden)
			}
		}
		if strings.Contains(strings.ToLower(request.URL.Path), "subjectaccessreview") {
			t.Errorf("observed forbidden SubjectAccessReview path %q", request.URL.Path)
		}
	}

	before := len(transport.requests)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		request, err := http.NewRequestWithContext(context.Background(), method, "https://cluster.invalid/api/v1/namespaces", nil)
		if err != nil {
			t.Fatalf("http.NewRequestWithContext(%s) error = %v", method, err)
		}
		_, err = session.httpClient.Do(request)
		requireHydrationError(t, err, ErrorKindMethodForbidden)
	}
	if got := len(transport.requests); got != before {
		t.Errorf("write requests reaching base transport = %d, want zero", got-before)
	}
}

func TestHydrationProductionSourcesExcludeImplicitAndMutatingMechanisms(t *testing.T) {
	t.Parallel()

	candidates, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("filepath.Glob() error = %v", err)
	}
	productionFiles := make([]string, 0, len(candidates)+1)
	for _, candidate := range candidates {
		if !strings.HasSuffix(candidate, "_test.go") {
			productionFiles = append(productionFiles, candidate)
		}
	}
	productionFiles = append(productionFiles, filepath.Join("..", "cli", "dependencies.go"))
	bannedText := []string{
		"SubjectAccessReview",
		"DryRun",
		"exec.Command",
		"kubectl",
		".Watch(",
	}
	bannedImports := map[string]bool{
		"k8s.io/client-go/kubernetes": true,
		"os/exec":                     true,
	}
	bannedImportPrefixes := []string{"k8s.io/client-go/informers", "k8s.io/client-go/tools/watch"}

	for _, name := range productionFiles {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("os.ReadFile(%s) error = %v", name, err)
		}
		for _, forbidden := range bannedText {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("%s contains forbidden hydration mechanism %q", name, forbidden)
			}
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, data, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parser.ParseFile(%s) error = %v", name, err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("strconv.Unquote(%s) error = %v", imported.Path.Value, err)
			}
			if bannedImports[path] {
				t.Errorf("%s imports forbidden hydration dependency %q", name, path)
			}
			for _, prefix := range bannedImportPrefixes {
				if path == prefix || strings.HasPrefix(path, prefix+"/") {
					t.Errorf("%s imports forbidden hydration dependency %q", name, path)
				}
			}
		}
	}
}

type auditRoundTripper struct {
	responses map[string]string
	mu        sync.Mutex
	requests  []*http.Request
}

func (transport *auditRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	transport.requests = append(transport.requests, request.Clone(request.Context()))
	transport.mu.Unlock()
	body, ok := transport.responses[request.URL.Path]
	if !ok {
		return nil, fmt.Errorf("unexpected Kubernetes API path %q", request.URL.Path)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func (transport *auditRoundTripper) paths() []string {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	paths := make([]string, len(transport.requests))
	for index, request := range transport.requests {
		paths[index] = request.URL.Path
	}
	sort.Strings(paths)
	return paths
}

func hydrationAuditResponses() map[string]string {
	return map[string]string{
		"/version":                              `{"gitVersion":"v1.36.2","major":"1","minor":"36"}`,
		"/api":                                  `{"kind":"APIVersions","apiVersion":"v1","versions":["v1"],"serverAddressByClientCIDRs":[]}`,
		"/apis":                                 `{"kind":"APIGroupList","apiVersion":"v1","groups":[{"name":"admissionregistration.k8s.io","versions":[{"groupVersion":"admissionregistration.k8s.io/v1","version":"v1"}],"preferredVersion":{"groupVersion":"admissionregistration.k8s.io/v1","version":"v1"}}]}`,
		"/api/v1":                               `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"v1","resources":[{"name":"pods","singularName":"","namespaced":true,"kind":"Pod","verbs":["create","get","list"]}]}`,
		"/apis/admissionregistration.k8s.io/v1": `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"admissionregistration.k8s.io/v1","resources":[{"name":"validatingwebhookconfigurations","singularName":"","namespaced":false,"kind":"ValidatingWebhookConfiguration","verbs":["create","get","list"]},{"name":"mutatingwebhookconfigurations","singularName":"","namespaced":false,"kind":"MutatingWebhookConfiguration","verbs":["create","get","list"]}]}`,
		"/api/v1/namespaces/team-a":             `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"team-a"}}`,
		"/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": `{"apiVersion":"admissionregistration.k8s.io/v1","kind":"ValidatingWebhookConfigurationList","items":[]}`,
		"/apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations":   `{"apiVersion":"admissionregistration.k8s.io/v1","kind":"MutatingWebhookConfigurationList","items":[]}`,
	}
}
