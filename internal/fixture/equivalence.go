package fixture

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var errEquivalenceFixtureMissing = errors.New("equivalence fixture is required")

type equivalenceKey struct {
	group       string
	version     string
	resource    string
	subresource string
}

// EquivalentResourceMapper resolves exact request resource keys exclusively
// from validated, immutable in-memory equivalence fixtures.
type EquivalentResourceMapper struct {
	equivalents map[equivalenceKey][]contract.EquivalentResource
}

// NewEquivalentResourceMapper validates and owns the supplied mappings.
// Equivalent target order is preserved. A non-nil empty equivalent list
// represents complete context with no equivalent targets.
func NewEquivalentResourceMapper(mappings []contract.EquivalenceMapping) (EquivalentResourceMapper, error) {
	mapper := EquivalentResourceMapper{
		equivalents: make(map[equivalenceKey][]contract.EquivalentResource, len(mappings)),
	}
	targetKinds := make(map[equivalenceKey]metav1.GroupVersionKind)
	for i, mapping := range mappings {
		mappingPath := fmt.Sprintf(".externalContext.equivalence[%d]", i)
		if err := validateResourceReference(mapping.Request, mappingPath+".request"); err != nil {
			return EquivalentResourceMapper{}, err
		}

		requestKey := keyFor(mapping.Request.GVR, mapping.Request.Subresource)
		if _, exists := mapper.equivalents[requestKey]; exists {
			return EquivalentResourceMapper{}, invalidEquivalence(
				mappingPath+".request",
				errors.New("duplicate request resource key"),
			)
		}
		if mapping.Equivalents == nil {
			return EquivalentResourceMapper{}, invalidEquivalence(
				mappingPath+".equivalents",
				errors.New("equivalent resource list must be provided"),
			)
		}

		owned := make([]contract.EquivalentResource, len(mapping.Equivalents))
		seenTargets := make(map[equivalenceKey]metav1.GroupVersionKind, len(mapping.Equivalents))
		for j, equivalent := range mapping.Equivalents {
			targetPath := fmt.Sprintf("%s.equivalents[%d]", mappingPath, j)
			if err := validateEquivalentResource(equivalent, targetPath); err != nil {
				return EquivalentResourceMapper{}, err
			}

			targetKey := keyFor(equivalent.GVR, equivalent.Subresource)
			if kind, exists := seenTargets[targetKey]; exists {
				if kind != equivalent.GVK {
					return EquivalentResourceMapper{}, invalidEquivalence(
						targetPath+".gvk",
						errors.New("equivalent resource key has conflicting kind"),
					)
				}
				return EquivalentResourceMapper{}, invalidEquivalence(targetPath, errors.New("duplicate equivalent resource key"))
			}
			seenTargets[targetKey] = equivalent.GVK

			if kind, exists := targetKinds[targetKey]; exists && kind != equivalent.GVK {
				return EquivalentResourceMapper{}, invalidEquivalence(
					targetPath+".gvk",
					errors.New("equivalent resource key has conflicting kind"),
				)
			}
			targetKinds[targetKey] = equivalent.GVK
			owned[j] = equivalent
		}
		mapper.equivalents[requestKey] = owned
	}
	// Structural fixture errors take precedence so an unsupported transform
	// cannot hide a duplicate or conflicting mapping.
	for i, mapping := range mappings {
		for j, equivalent := range mapping.Equivalents {
			if kube136.EquivalentSubresourceSupported(mapping.Request.Subresource, equivalent.Subresource) {
				continue
			}
			targetPath := fmt.Sprintf(".externalContext.equivalence[%d].equivalents[%d]", i, j)
			return EquivalentResourceMapper{}, &contract.UnsupportedCapabilityError{
				Capability: "cross-subresource equivalence at " + targetPath + ".subresource",
				Err:        errors.New("target subresource differs from request subresource"),
			}
		}
	}
	return mapper, nil
}

// Lookup returns a caller-owned ordered target list for request. Matching is
// exact and case-sensitive across GVR and subresource. A missing request key is
// classified as contract.ErrMissingContext.
func (m EquivalentResourceMapper) Lookup(request contract.ResourceReference) ([]contract.EquivalentResource, error) {
	equivalents, ok := m.equivalents[keyFor(request.GVR, request.Subresource)]
	if !ok {
		return nil, &contract.MissingContextError{
			Context:   "equivalence",
			Reference: resourceReference(request),
			Err:       errEquivalenceFixtureMissing,
		}
	}
	owned := make([]contract.EquivalentResource, len(equivalents))
	copy(owned, equivalents)
	return owned, nil
}

func validateResourceReference(reference contract.ResourceReference, path string) error {
	if err := validateGVR(reference.GVR, path+".gvr"); err != nil {
		return err
	}
	return validateSubresource(reference.Subresource, path+".subresource")
}

func validateEquivalentResource(resource contract.EquivalentResource, path string) error {
	if err := validateGVR(resource.GVR, path+".gvr"); err != nil {
		return err
	}
	if err := validateGVK(resource.GVK, path+".gvk"); err != nil {
		return err
	}
	return validateSubresource(resource.Subresource, path+".subresource")
}

func validateGVR(gvr metav1.GroupVersionResource, path string) error {
	if err := validateIdentifier(gvr.Group, false, path+".group"); err != nil {
		return err
	}
	if err := validateIdentifier(gvr.Version, true, path+".version"); err != nil {
		return err
	}
	return validateIdentifier(gvr.Resource, true, path+".resource")
}

func validateGVK(gvk metav1.GroupVersionKind, path string) error {
	if err := validateIdentifier(gvk.Group, false, path+".group"); err != nil {
		return err
	}
	if err := validateIdentifier(gvk.Version, true, path+".version"); err != nil {
		return err
	}
	return validateIdentifier(gvk.Kind, true, path+".kind")
}

func validateSubresource(subresource, path string) error {
	if strings.ContainsRune(subresource, '/') {
		return invalidEquivalence(path, errors.New("subresource must not contain a slash"))
	}
	if strings.IndexFunc(subresource, unicode.IsSpace) >= 0 {
		return invalidEquivalence(path, errors.New("subresource must not contain whitespace"))
	}
	return nil
}

func validateIdentifier(value string, required bool, path string) error {
	if required && value == "" {
		return invalidEquivalence(path, errors.New("value must not be empty"))
	}
	if strings.ContainsRune(value, '/') {
		return invalidEquivalence(path, errors.New("value must not contain a slash"))
	}
	if strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return invalidEquivalence(path, errors.New("value must not contain whitespace"))
	}
	return nil
}

func keyFor(gvr metav1.GroupVersionResource, subresource string) equivalenceKey {
	return equivalenceKey{
		group:       gvr.Group,
		version:     gvr.Version,
		resource:    gvr.Resource,
		subresource: subresource,
	}
}

func resourceReference(reference contract.ResourceReference) string {
	return fmt.Sprintf("%s/%s/%s/%s", reference.GVR.Group, reference.GVR.Version, reference.GVR.Resource, reference.Subresource)
}

func invalidEquivalence(field string, err error) error {
	return &contract.InvalidInputError{Field: field, Err: err}
}
