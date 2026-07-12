package kube136

import (
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	stagingrules "k8s.io/apiserver/pkg/admission/plugin/webhook/predicates/rules"
)

// ExactRuleMatches evaluates one admission rule with the Kubernetes 1.36
// staging matcher. The explicit namespaced flag is translated to the namespace
// presence that admission.Attributes uses to represent resource scope.
func ExactRuleMatches(
	rule admissionregistrationv1.RuleWithOperations,
	operation admissionv1.Operation,
	resource schema.GroupVersionResource,
	subresource string,
	namespaced bool,
) bool {
	namespace := metav1.NamespaceNone
	if namespaced {
		namespace = metav1.NamespaceDefault
	}
	attributes := admission.NewAttributesRecord(
		nil,
		nil,
		schema.GroupVersionKind{},
		namespace,
		"",
		resource,
		subresource,
		admission.Operation(operation),
		nil,
		false,
		nil,
	)
	return (&stagingrules.Matcher{Rule: rule, Attr: attributes}).Matches()
}
