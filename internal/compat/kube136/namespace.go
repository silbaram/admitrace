package kube136

import (
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// NamespaceContextMode identifies how Kubernetes 1.36 obtains namespace
// metadata before evaluating a webhook namespace selector.
type NamespaceContextMode string

const (
	// NamespaceContextModeRequestObject uses request.object for a Namespace CREATE or UPDATE.
	NamespaceContextModeRequestObject NamespaceContextMode = "request-object"
	// NamespaceContextModeFixture requires namespace metadata from external context.
	NamespaceContextModeFixture NamespaceContextMode = "fixture"
	// NamespaceContextModeNotRequired bypasses namespace lookup for a cluster-scoped, non-Namespace resource.
	NamespaceContextModeNotRequired NamespaceContextMode = "not-required"
)

// NamespaceContextModeFor returns the Kubernetes 1.36 namespace context path
// for one admission request. The namespaced flag is supplied by the caller so
// this compatibility decision does not depend on the product contract model.
func NamespaceContextModeFor(
	operation admissionv1.Operation,
	resource schema.GroupVersionResource,
	subresource string,
	namespaced bool,
) NamespaceContextMode {
	isNamespace := resource.Resource == "namespaces"
	if isNamespace && subresource == "" && (operation == admissionv1.Create || operation == admissionv1.Update) {
		return NamespaceContextModeRequestObject
	}
	if !namespaced && !isNamespace {
		return NamespaceContextModeNotRequired
	}
	return NamespaceContextModeFixture
}
