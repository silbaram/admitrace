package kube136

import (
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var errNamespaceLabelsLoaderRequired = errors.New("namespace labels loader is required")

// NamespaceLabels loads the namespace labels required by a non-empty
// namespace selector. The callback keeps fixture access outside the
// Kubernetes compatibility package.
type NamespaceLabels func() (map[string]string, error)

// NamespaceSelectorMatches evaluates a namespace selector in Kubernetes 1.36
// order. Cluster-scoped non-Namespace requests and empty selectors do not load
// namespace context.
func NamespaceSelectorMatches(
	selector *metav1.LabelSelector,
	mode NamespaceContextMode,
	loadLabels NamespaceLabels,
) (bool, error) {
	if mode == NamespaceContextModeNotRequired {
		return true, nil
	}
	// The admissionregistration API defaults an omitted selector to an empty
	// selector. AdmiTrace preserves nil through normalization, so apply that
	// routing default at this compatibility leaf before Kubernetes parsing.
	if selector == nil {
		return true, nil
	}

	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, err
	}
	if parsed.Empty() {
		return true, nil
	}
	if loadLabels == nil {
		return false, errNamespaceLabelsLoaderRequired
	}

	namespaceLabels, err := loadLabels()
	if err != nil {
		return false, err
	}
	return parsed.Matches(labels.Set(namespaceLabels)), nil
}

// ObjectSelectorMatches evaluates the selector against object and oldObject
// labels with the Kubernetes 1.36 OR semantics. A false presence flag models
// an absent or null payload and cannot satisfy a non-empty selector.
func ObjectSelectorMatches(
	selector *metav1.LabelSelector,
	objectLabels map[string]string,
	objectPresent bool,
	oldObjectLabels map[string]string,
	oldObjectPresent bool,
) (bool, error) {
	// See NamespaceSelectorMatches for why nil is match-all at this boundary.
	if selector == nil {
		return true, nil
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, err
	}
	if parsed.Empty() {
		return true, nil
	}
	return matchesObjectLabels(parsed, objectLabels, objectPresent) ||
		matchesObjectLabels(parsed, oldObjectLabels, oldObjectPresent), nil
}

func matchesObjectLabels(selector labels.Selector, objectLabels map[string]string, present bool) bool {
	return present && selector.Matches(labels.Set(objectLabels))
}
