package kube136_test

import (
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsExemptAdmissionConfigurationResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind schema.GroupVersionKind
		want bool
	}{
		{name: "validating webhook configuration", kind: admissionKind("ValidatingWebhookConfiguration"), want: true},
		{name: "mutating webhook configuration", kind: admissionKind("MutatingWebhookConfiguration"), want: true},
		{name: "validating admission policy", kind: admissionKind("ValidatingAdmissionPolicy"), want: true},
		{name: "validating admission policy binding", kind: admissionKind("ValidatingAdmissionPolicyBinding"), want: true},
		{name: "mutating admission policy", kind: admissionKind("MutatingAdmissionPolicy"), want: true},
		{name: "mutating admission policy binding", kind: admissionKind("MutatingAdmissionPolicyBinding"), want: true},
		{name: "wrong group", kind: schema.GroupVersionKind{Group: "example.io", Kind: "ValidatingWebhookConfiguration"}},
		{name: "wrong kind", kind: admissionKind("AdmissionReview")},
		{name: "resource-shaped kind is not exempt", kind: admissionKind("validatingwebhookconfigurations")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := kube136.IsExemptAdmissionConfigurationResource(test.kind); got != test.want {
				t.Errorf("IsExemptAdmissionConfigurationResource(%#v) = %t, want %t", test.kind, got, test.want)
			}
		})
	}
}

func admissionKind(kind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: kind}
}
