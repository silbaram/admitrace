package kube136_test

import (
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	corelisters "k8s.io/client-go/listers/core/v1"
)

var (
	_ *admissionregistrationv1.MutatingWebhookConfiguration   = (*kube136.MutatingWebhookConfiguration)(nil)
	_ *admissionregistrationv1.ValidatingWebhookConfiguration = (*kube136.ValidatingWebhookConfiguration)(nil)
	_ schema.GroupVersionResource                             = kube136.GroupVersionResource{}
	_ schema.GroupVersionKind                                 = kube136.GroupVersionKind{}
	_ admission.ObjectInterfaces                              = kube136.ObjectInterfaces(nil)
	_ corelisters.NamespaceLister                             = kube136.NamespaceLister(nil)
)

func TestVersionConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "Kubernetes version", got: kube136.KubernetesVersion, want: "1.36.2"},
		{name: "module version", got: kube136.ModuleVersion, want: "v0.36.2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Errorf("version = %q, want %q", test.got, test.want)
			}
		})
	}
}
