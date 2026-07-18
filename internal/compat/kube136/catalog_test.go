package kube136_test

import (
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestLookupResourceUsesOnlyExactGVK(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		gvk        schema.GroupVersionKind
		wantFound  bool
		wantPlural string
		wantScope  bool
	}{
		{name: "core Pod", gvk: schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, wantFound: true, wantPlural: "pods", wantScope: true},
		{name: "apps Deployment", gvk: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, wantFound: true, wantPlural: "deployments", wantScope: true},
		{name: "wrong case", gvk: schema.GroupVersionKind{Version: "v1", Kind: "pod"}},
		{name: "missing group", gvk: schema.GroupVersionKind{Version: "v1", Kind: "Deployment"}},
		{name: "unknown CRD", gvk: schema.GroupVersionKind{Group: "widgets.example.com", Version: "v1", Kind: "Widget"}},
		{name: "unknown kind that could be pluralized", gvk: schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Policy"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			resource, found := kube136.LookupResource(test.gvk)
			if found != test.wantFound {
				t.Fatalf("LookupResource(%s) found = %t, want %t", test.gvk, found, test.wantFound)
			}
			if found && (resource.Resource != test.wantPlural || resource.Namespaced != test.wantScope) {
				t.Errorf("LookupResource(%s) = %#v, want plural/scope (%q, %t)", test.gvk, resource, test.wantPlural, test.wantScope)
			}
		})
	}
}
