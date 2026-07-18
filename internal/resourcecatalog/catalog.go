package resourcecatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// SchemaVersion is the stable committed resource catalog schema.
const SchemaVersion = "admitrace.resource-catalog/v1alpha1"

// Catalog is a profile-bound, canonical list of creatable built-in resources.
type Catalog struct {
	SchemaVersion     string     `json:"schemaVersion"`
	ProfileID         string     `json:"profileId"`
	KubernetesVersion string     `json:"kubernetesVersion"`
	Resources         []Resource `json:"resources"`
}

// Resource maps one exact GVK to its discovered GVR and scope.
type Resource struct {
	Group      string `json:"group"`
	Version    string `json:"version"`
	Kind       string `json:"kind"`
	Resource   string `json:"resource"`
	Namespaced bool   `json:"namespaced"`
}

// GVK returns the exact lookup key for the catalog entry.
func (resource Resource) GVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: resource.Group, Version: resource.Version, Kind: resource.Kind}
}

// GVR returns the exact discovered resource identifier.
func (resource Resource) GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: resource.Group, Version: resource.Version, Resource: resource.Resource}
}

// Generate builds a canonical catalog from Kubernetes discovery. When
// builtInGroupVersions is non-empty, resources outside that clean-server
// baseline are excluded so CRD-discovered APIs cannot enter the embedded
// catalog.
func Generate(profileID, kubernetesVersion string, lists []*metav1.APIResourceList, builtInGroupVersions map[string]struct{}) (Catalog, error) {
	resources := make([]Resource, 0)
	for _, list := range lists {
		if list == nil {
			continue
		}
		if len(builtInGroupVersions) > 0 {
			if _, ok := builtInGroupVersions[list.GroupVersion]; !ok {
				continue
			}
		}
		groupVersion, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			return Catalog{}, fmt.Errorf("parse discovery groupVersion %q: %w", list.GroupVersion, err)
		}
		for _, apiResource := range list.APIResources {
			if strings.Contains(apiResource.Name, "/") || apiResource.Kind == "" || !slices.Contains(apiResource.Verbs, "create") {
				continue
			}
			resources = append(resources, Resource{
				Group:      groupVersion.Group,
				Version:    groupVersion.Version,
				Kind:       apiResource.Kind,
				Resource:   apiResource.Name,
				Namespaced: apiResource.Namespaced,
			})
		}
	}
	sort.Slice(resources, func(i, j int) bool {
		return compareResource(resources[i], resources[j]) < 0
	})
	catalog := Catalog{
		SchemaVersion:     SchemaVersion,
		ProfileID:         profileID,
		KubernetesVersion: kubernetesVersion,
		Resources:         resources,
	}
	if err := catalog.Validate(profileID, kubernetesVersion); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

// BuiltInGroupVersions captures the clean-server discovery baseline used to
// distinguish built-in APIs from resources installed later through CRDs.
func BuiltInGroupVersions(lists []*metav1.APIResourceList) map[string]struct{} {
	result := make(map[string]struct{}, len(lists))
	for _, list := range lists {
		if list != nil && list.GroupVersion != "" {
			result[list.GroupVersion] = struct{}{}
		}
	}
	return result
}

// Validate checks profile identity, entry completeness, duplicate GVKs, and
// canonical ordering.
func (catalog Catalog) Validate(profileID, kubernetesVersion string) error {
	if catalog.SchemaVersion != SchemaVersion {
		return fmt.Errorf("catalog schema version got %q, want %q", catalog.SchemaVersion, SchemaVersion)
	}
	if catalog.ProfileID != profileID {
		return fmt.Errorf("catalog profile drift: got %q, want %q", catalog.ProfileID, profileID)
	}
	if catalog.KubernetesVersion != kubernetesVersion {
		return fmt.Errorf("catalog Kubernetes version drift: got %q, want %q", catalog.KubernetesVersion, kubernetesVersion)
	}
	if len(catalog.Resources) == 0 {
		return errors.New("catalog contains no resources")
	}
	seen := make(map[schema.GroupVersionKind]struct{}, len(catalog.Resources))
	for index, resource := range catalog.Resources {
		if resource.Version == "" || resource.Kind == "" || resource.Resource == "" {
			return fmt.Errorf("catalog resource %d has an empty version, kind, or resource", index)
		}
		if strings.Contains(resource.Resource, "/") {
			return fmt.Errorf("catalog resource %s contains a subresource", describe(resource))
		}
		if _, ok := seen[resource.GVK()]; ok {
			return fmt.Errorf("catalog contains duplicate GVK %s", resource.GVK())
		}
		seen[resource.GVK()] = struct{}{}
		if index > 0 && compareResource(catalog.Resources[index-1], resource) >= 0 {
			return fmt.Errorf("catalog resources are not in canonical order at %s", describe(resource))
		}
	}
	return nil
}

// Marshal returns the byte-stable committed representation.
func Marshal(catalog Catalog) ([]byte, error) {
	encoded, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal resource catalog: %w", err)
	}
	return append(encoded, '\n'), nil
}

// Parse strictly decodes and validates one committed catalog.
func Parse(data []byte, profileID, kubernetesVersion string) (Catalog, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var catalog Catalog
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode resource catalog: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Catalog{}, errors.New("decode resource catalog: trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return Catalog{}, fmt.Errorf("decode resource catalog trailing data: %w", err)
	}
	if err := catalog.Validate(profileID, kubernetesVersion); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

// Compare returns every semantic drift between the committed and regenerated
// catalogs. A byte comparison should additionally be used to detect formatting
// drift after this semantic check passes.
func Compare(committed, regenerated Catalog) error {
	if err := committed.Validate(committed.ProfileID, committed.KubernetesVersion); err != nil {
		return fmt.Errorf("committed catalog: %w", err)
	}
	if err := regenerated.Validate(committed.ProfileID, committed.KubernetesVersion); err != nil {
		return fmt.Errorf("regenerated catalog: %w", err)
	}
	committedByGVK := indexByGVK(committed.Resources)
	regeneratedByGVK := indexByGVK(regenerated.Resources)
	issues := make([]string, 0)
	for key, expected := range committedByGVK {
		actual, ok := regeneratedByGVK[key]
		if !ok {
			issues = append(issues, "missing "+key.String())
			continue
		}
		if actual.Resource != expected.Resource {
			issues = append(issues, fmt.Sprintf("wrong plural for %s: got %q, want %q", key, actual.Resource, expected.Resource))
		}
		if actual.Namespaced != expected.Namespaced {
			issues = append(issues, fmt.Sprintf("wrong scope for %s: got namespaced=%t, want %t", key, actual.Namespaced, expected.Namespaced))
		}
	}
	for key := range regeneratedByGVK {
		if _, ok := committedByGVK[key]; !ok {
			issues = append(issues, "extra "+key.String())
		}
	}
	if len(issues) > 0 {
		sort.Strings(issues)
		return errors.New("resource catalog drift: " + strings.Join(issues, "; "))
	}
	return nil
}

func compareResource(left, right Resource) int {
	leftKey := []string{left.Group, left.Version, left.Kind, left.Resource}
	rightKey := []string{right.Group, right.Version, right.Kind, right.Resource}
	for index := range leftKey {
		if comparison := strings.Compare(leftKey[index], rightKey[index]); comparison != 0 {
			return comparison
		}
	}
	if left.Namespaced == right.Namespaced {
		return 0
	}
	if !left.Namespaced {
		return -1
	}
	return 1
}

func indexByGVK(resources []Resource) map[schema.GroupVersionKind]Resource {
	result := make(map[schema.GroupVersionKind]Resource, len(resources))
	for _, resource := range resources {
		result[resource.GVK()] = resource
	}
	return result
}

func describe(resource Resource) string {
	return fmt.Sprintf("%s -> %s", resource.GVK(), resource.GVR())
}
