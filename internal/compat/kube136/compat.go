// Package kube136 isolates Kubernetes 1.36 compatibility details.
package kube136

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	corelisters "k8s.io/client-go/listers/core/v1"
)

const (
	// ProfileID is the stable identity of the Kubernetes 1.36.2 default profile.
	ProfileID = "kubernetes-1.36.2-defaults"
	// KubernetesVersion identifies the supported Kubernetes release.
	KubernetesVersion = "1.36.2"
	// ModuleVersion identifies the aligned Kubernetes module release.
	ModuleVersion = "v0.36.2"
)

// MutatingWebhookConfiguration is the Kubernetes 1.36 mutating webhook configuration type.
type MutatingWebhookConfiguration = admissionregistrationv1.MutatingWebhookConfiguration

// ValidatingWebhookConfiguration is the Kubernetes 1.36 validating webhook configuration type.
type ValidatingWebhookConfiguration = admissionregistrationv1.ValidatingWebhookConfiguration

// GroupVersionResource identifies a Kubernetes API resource.
type GroupVersionResource = schema.GroupVersionResource

// GroupVersionKind identifies a Kubernetes API kind.
type GroupVersionKind = schema.GroupVersionKind

// ObjectInterfaces provides admission plugins with Kubernetes object operations.
type ObjectInterfaces = admission.ObjectInterfaces

// NamespaceLister lists and retrieves Kubernetes namespaces from a shared cache.
type NamespaceLister = corelisters.NamespaceLister
