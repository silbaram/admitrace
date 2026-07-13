package oracle

import (
	"context"
	"fmt"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
)

const equivalentGroup = "equivalent.oracle.admitrace.io"

// EquivalentResource describes the two served versions used by Equivalent tests.
type EquivalentResource struct {
	Name           string
	Kind           string
	Group          string
	Resource       string
	StorageVersion string
	OtherVersion   string
	StorageGVR     schema.GroupVersionResource
	OtherGVR       schema.GroupVersionResource
}

// InstallEquivalentResource installs a namespaced, two-version CRD with explicit storage and conversion semantics.
func (h *Harness) InstallEquivalentResource(ctx context.Context, caseName string) (EquivalentResource, func(context.Context) error, error) {
	suffix := stableSuffix(caseName)
	plural := "widgets" + suffix
	name := plural + "." + equivalentGroup
	resource := EquivalentResource{
		Name:           name,
		Kind:           "Widget" + suffix,
		Group:          equivalentGroup,
		Resource:       plural,
		StorageVersion: "v1alpha1",
		OtherVersion:   "v1beta1",
		StorageGVR:     schema.GroupVersionResource{Group: equivalentGroup, Version: "v1alpha1", Resource: plural},
		OtherGVR:       schema.GroupVersionResource{Group: equivalentGroup, Version: "v1beta1", Resource: plural},
	}
	object := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: equivalentGroup,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   plural,
				Singular: "widget" + suffix,
				Kind:     resource.Kind,
				ListKind: resource.Kind + "List",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{Name: resource.StorageVersion, Served: true, Storage: true, Schema: permissiveSchema()},
				{Name: resource.OtherVersion, Served: true, Storage: false, Schema: permissiveSchema()},
			},
			Conversion: &apiextensionsv1.CustomResourceConversion{Strategy: apiextensionsv1.NoneConverter},
		},
	}
	if _, err := h.Extensions.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, object, metav1.CreateOptions{}); err != nil {
		return EquivalentResource{}, nil, &SetupError{Stage: SetupResource, Err: fmt.Errorf("create equivalent CRD %s: %w", name, err)}
	}
	cleanup := func(ctx context.Context) error {
		err := h.Extensions.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete equivalent CRD %s: %w", name, err)
		}
		return nil
	}
	h.track(cleanup)

	err := wait.PollUntilContextTimeout(ctx, 25*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		current, err := h.Extensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return EquivalentResource{}, cleanup, &SetupError{Stage: SetupResource, Err: fmt.Errorf("wait for equivalent CRD %s: %w", name, err)}
	}
	return resource, cleanup, nil
}

func permissiveSchema() *apiextensionsv1.CustomResourceValidation {
	return &apiextensionsv1.CustomResourceValidation{
		OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
			Type:                   "object",
			XPreserveUnknownFields: pointer(true),
		},
	}
}

func pointer[T any](value T) *T {
	return &value
}

func stableSuffix(value string) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	var hash uint32 = 2166136261
	for index := 0; index < len(value); index++ {
		hash ^= uint32(value[index])
		hash *= 16777619
	}
	result := make([]byte, 8)
	for index := range result {
		result[index] = alphabet[hash%uint32(len(alphabet))]
		hash /= uint32(len(alphabet))
	}
	return string(result)
}
