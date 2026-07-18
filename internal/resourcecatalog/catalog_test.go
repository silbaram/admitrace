package resourcecatalog_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/resourcecatalog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	testProfile = "kubernetes-1.36.2-defaults"
	testVersion = "1.36.2"
)

func TestGenerateIsDeterministicAndFiltersDiscovery(t *testing.T) {
	t.Parallel()

	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{
				{Name: "deployments/status", Kind: "Deployment", Namespaced: true, Verbs: metav1.Verbs{"get", "update"}},
				{Name: "deployments", Kind: "Deployment", Namespaced: true, Verbs: metav1.Verbs{"list", "create"}},
			},
		},
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "nodes", Kind: "Node", Verbs: metav1.Verbs{"create"}},
				{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: metav1.Verbs{"create"}},
				{Name: "bindings", Kind: "Binding", Namespaced: true, Verbs: metav1.Verbs{"get"}},
			},
		},
		{
			GroupVersion: "widgets.example.com/v1",
			APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: metav1.Verbs{"create"}}},
		},
	}
	builtIns := map[string]struct{}{"v1": {}, "apps/v1": {}}

	first, err := resourcecatalog.Generate(testProfile, testVersion, lists, builtIns)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	second, err := resourcecatalog.Generate(testProfile, testVersion, lists, builtIns)
	if err != nil {
		t.Fatalf("Generate() second error = %v", err)
	}
	firstBytes, err := resourcecatalog.Marshal(first)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	secondBytes, err := resourcecatalog.Marshal(second)
	if err != nil {
		t.Fatalf("Marshal() second error = %v", err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("repeated generation is not byte stable")
	}
	want := []schema.GroupVersionKind{
		{Version: "v1", Kind: "Node"},
		{Version: "v1", Kind: "Pod"},
		{Group: "apps", Version: "v1", Kind: "Deployment"},
	}
	if len(first.Resources) != len(want) {
		t.Fatalf("resources = %#v, want %d built-in creatable roots", first.Resources, len(want))
	}
	for index, resource := range first.Resources {
		if resource.GVK() != want[index] {
			t.Errorf("resource %d GVK = %s, want %s", index, resource.GVK(), want[index])
		}
	}
}

func TestCatalogValidationRejectsStructuralDrift(t *testing.T) {
	t.Parallel()

	base := testCatalog()
	tests := []struct {
		name    string
		mutate  func(*resourcecatalog.Catalog)
		wantErr string
	}{
		{name: "profile", mutate: func(catalog *resourcecatalog.Catalog) { catalog.ProfileID = "other" }, wantErr: "profile drift"},
		{name: "version", mutate: func(catalog *resourcecatalog.Catalog) { catalog.KubernetesVersion = "1.36.1" }, wantErr: "version drift"},
		{name: "duplicate", mutate: func(catalog *resourcecatalog.Catalog) {
			catalog.Resources = append(catalog.Resources, catalog.Resources[1])
		}, wantErr: "duplicate GVK"},
		{name: "nondeterministic order", mutate: func(catalog *resourcecatalog.Catalog) {
			catalog.Resources[0], catalog.Resources[1] = catalog.Resources[1], catalog.Resources[0]
		}, wantErr: "canonical order"},
		{name: "subresource", mutate: func(catalog *resourcecatalog.Catalog) { catalog.Resources[0].Resource = "pods/status" }, wantErr: "subresource"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			catalog := base
			catalog.Resources = append([]resourcecatalog.Resource(nil), base.Resources...)
			test.mutate(&catalog)
			err := catalog.Validate(testProfile, testVersion)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestCompareReportsFullSemanticDrift(t *testing.T) {
	t.Parallel()

	committed := testCatalog()
	tests := []struct {
		name    string
		mutate  func(*resourcecatalog.Catalog)
		wantErr string
	}{
		{name: "missing", mutate: func(catalog *resourcecatalog.Catalog) { catalog.Resources = catalog.Resources[1:] }, wantErr: "missing /v1, Kind=Node"},
		{name: "extra", mutate: func(catalog *resourcecatalog.Catalog) {
			catalog.Resources = append(catalog.Resources, resourcecatalog.Resource{Group: "batch", Version: "v1", Kind: "Job", Resource: "jobs", Namespaced: true})
		}, wantErr: "extra batch/v1, Kind=Job"},
		{name: "plural", mutate: func(catalog *resourcecatalog.Catalog) { catalog.Resources[1].Resource = "podses" }, wantErr: "wrong plural"},
		{name: "scope", mutate: func(catalog *resourcecatalog.Catalog) { catalog.Resources[1].Namespaced = false }, wantErr: "wrong scope"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			regenerated := committed
			regenerated.Resources = append([]resourcecatalog.Resource(nil), committed.Resources...)
			test.mutate(&regenerated)
			err := resourcecatalog.Compare(committed, regenerated)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Compare() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestParseIsStrict(t *testing.T) {
	t.Parallel()

	encoded, err := resourcecatalog.Marshal(testCatalog())
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if _, err := resourcecatalog.Parse(encoded, testProfile, testVersion); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	unknown := bytes.Replace(encoded, []byte(`"resources":`), []byte(`"unknown":true,"resources":`), 1)
	if _, err := resourcecatalog.Parse(unknown, testProfile, testVersion); err == nil {
		t.Fatal("Parse() accepted unknown field")
	}
}

func testCatalog() resourcecatalog.Catalog {
	return resourcecatalog.Catalog{
		SchemaVersion:     resourcecatalog.SchemaVersion,
		ProfileID:         testProfile,
		KubernetesVersion: testVersion,
		Resources: []resourcecatalog.Resource{
			{Version: "v1", Kind: "Node", Resource: "nodes"},
			{Version: "v1", Kind: "Pod", Resource: "pods", Namespaced: true},
		},
	}
}
