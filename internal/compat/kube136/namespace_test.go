package kube136_test

import (
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNamespaceContextModeFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		operation   admissionv1.Operation
		resource    schema.GroupVersionResource
		subresource string
		namespaced  bool
		want        kube136.NamespaceContextMode
	}{
		{
			name:      "Namespace create uses request object",
			operation: admissionv1.Create,
			resource:  schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
			want:      kube136.NamespaceContextModeRequestObject,
		},
		{
			name:      "Namespace update uses request object",
			operation: admissionv1.Update,
			resource:  schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
			want:      kube136.NamespaceContextModeRequestObject,
		},
		{
			name:      "Namespace delete uses fixture",
			operation: admissionv1.Delete,
			resource:  schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
			want:      kube136.NamespaceContextModeFixture,
		},
		{
			name:        "Namespace subresource uses fixture",
			operation:   admissionv1.Update,
			resource:    schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
			subresource: "finalize",
			want:        kube136.NamespaceContextModeFixture,
		},
		{
			name:       "namespaced resource uses fixture",
			operation:  admissionv1.Create,
			resource:   schema.GroupVersionResource{Version: "v1", Resource: "pods"},
			namespaced: true,
			want:       kube136.NamespaceContextModeFixture,
		},
		{
			name:      "cluster scoped non-Namespace needs no context",
			operation: admissionv1.Create,
			resource:  schema.GroupVersionResource{Version: "v1", Resource: "nodes"},
			want:      kube136.NamespaceContextModeNotRequired,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := kube136.NamespaceContextModeFor(test.operation, test.resource, test.subresource, test.namespaced)
			if got != test.want {
				t.Errorf("NamespaceContextModeFor() = %q, want %q", got, test.want)
			}
		})
	}
}
