package kube136

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	stagingrules "k8s.io/apiserver/pkg/admission/plugin/webhook/predicates/rules"
)

// IsExemptAdmissionConfigurationResource reports whether Kubernetes 1.36
// excludes the requested kind before dispatching API-sourced webhooks.
func IsExemptAdmissionConfigurationResource(kind schema.GroupVersionKind) bool {
	attributes := admission.NewAttributesRecord(
		nil,
		nil,
		kind,
		"",
		"",
		schema.GroupVersionResource{},
		"",
		admission.Create,
		nil,
		false,
		nil,
	)
	return stagingrules.IsExemptAdmissionConfigurationResource(attributes)
}
