package kube136

import (
	_ "embed"

	"github.com/silbaram/admitrace/internal/resourcecatalog"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

//go:embed resource_catalog.json
var embeddedResourceCatalog []byte

var resourcesByGVK = loadResourceCatalog()

// LookupResource resolves one exact built-in GVK from the committed Kubernetes
// 1.36.2 discovery catalog. It never pluralizes or infers scope.
func LookupResource(gvk schema.GroupVersionKind) (resourcecatalog.Resource, bool) {
	resource, ok := resourcesByGVK[gvk]
	return resource, ok
}

func loadResourceCatalog() map[schema.GroupVersionKind]resourcecatalog.Resource {
	catalog, err := resourcecatalog.Parse(embeddedResourceCatalog, ProfileID, KubernetesVersion)
	if err != nil {
		panic("load embedded Kubernetes 1.36.2 resource catalog: " + err.Error())
	}
	resources := make(map[schema.GroupVersionKind]resourcecatalog.Resource, len(catalog.Resources))
	for _, resource := range catalog.Resources {
		resources[resource.GVK()] = resource
	}
	return resources
}
