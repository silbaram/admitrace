package fixture_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEquivalentResourceMapperLookupPreservesOrderAndOwnership(t *testing.T) {
	t.Parallel()

	deployments := resourceReference("apps", "v1", "deployments", "scale")
	pods := resourceReference("", "v1", "pods", "")
	mappings := []contract.EquivalenceMapping{
		{
			Request: deployments,
			Equivalents: []contract.EquivalentResource{
				equivalentResource("extensions", "v1beta1", "deployments", "autoscaling", "v1", "Scale", "scale"),
				equivalentResource("apps", "v1beta1", "deployments", "apps", "v1beta1", "Scale", "scale"),
				equivalentResource("apps", "v1alpha1", "deployments", "apps", "v1alpha1", "Scale", "scale"),
			},
		},
		{
			Request: pods,
			Equivalents: []contract.EquivalentResource{
				equivalentResource("", "v1", "pods", "", "v1", "Pod", ""),
			},
		},
	}
	wantDeployments := append([]contract.EquivalentResource(nil), mappings[0].Equivalents...)
	wantPods := append([]contract.EquivalentResource(nil), mappings[1].Equivalents...)
	mapper := mustEquivalentResourceMapper(t, mappings)

	// Constructor ownership must isolate the mapper from later fixture changes.
	mappings[0].Equivalents[0].GVR.Version = "mutated-input"
	mappings[0].Equivalents[1].GVK.Kind = "MutatedInput"
	mappings[1].Equivalents[0].Subresource = "mutated-input"

	for i := 0; i < 25; i++ {
		got, err := mapper.Lookup(deployments)
		if err != nil {
			t.Fatalf("Lookup(deployments) iteration %d error = %v", i, err)
		}
		if !reflect.DeepEqual(got, wantDeployments) {
			t.Fatalf("Lookup(deployments) iteration %d = %#v, want %#v", i, got, wantDeployments)
		}
	}

	first, err := mapper.Lookup(deployments)
	if err != nil {
		t.Fatalf("Lookup(deployments) error = %v", err)
	}
	first[0].GVR.Version = "mutated-result"
	first[1].GVK.Kind = "MutatedResult"
	first[2].Subresource = "mutated-result"
	second, err := mapper.Lookup(deployments)
	if err != nil {
		t.Fatalf("second Lookup(deployments) error = %v", err)
	}
	if !reflect.DeepEqual(second, wantDeployments) {
		t.Errorf("second Lookup(deployments) = %#v, want owned result %#v", second, wantDeployments)
	}

	gotPods, err := mapper.Lookup(pods)
	if err != nil {
		t.Fatalf("Lookup(pods) error = %v", err)
	}
	if !reflect.DeepEqual(gotPods, wantPods) {
		t.Errorf("Lookup(pods) = %#v, want isolated mapping %#v", gotPods, wantPods)
	}
}

func TestEquivalentResourceMapperLookupIsExactAndCaseSensitive(t *testing.T) {
	t.Parallel()

	request := resourceReference("apps", "v1", "deployments", "status")
	mapper := mustEquivalentResourceMapper(t, []contract.EquivalenceMapping{{
		Request: request,
		Equivalents: []contract.EquivalentResource{
			equivalentResource("apps", "v1beta1", "deployments", "apps", "v1beta1", "Deployment", "status"),
		},
	}})

	tests := []struct {
		name    string
		request contract.ResourceReference
	}{
		{name: "group case", request: resourceReference("Apps", "v1", "deployments", "status")},
		{name: "version case", request: resourceReference("apps", "V1", "deployments", "status")},
		{name: "resource case", request: resourceReference("apps", "v1", "Deployments", "status")},
		{name: "subresource case", request: resourceReference("apps", "v1", "deployments", "Status")},
		{name: "different subresource", request: resourceReference("apps", "v1", "deployments", "scale")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := mapper.Lookup(test.request)
			if got != nil {
				t.Errorf("Lookup() = %#v, want nil", got)
			}
			if !errors.Is(err, contract.ErrMissingContext) {
				t.Fatalf("Lookup() error = %v, want ErrMissingContext", err)
			}
			if errors.Is(err, contract.ErrInvalidInput) || errors.Is(err, contract.ErrUnsupportedCapability) {
				t.Errorf("Lookup() error = %v, want only missing-context category", err)
			}
			var missing *contract.MissingContextError
			if !errors.As(err, &missing) {
				t.Fatalf("Lookup() error type = %T, want *contract.MissingContextError", err)
			}
			if missing.Context != "equivalence" || missing.Reference == "" {
				t.Errorf("MissingContextError = %#v, want equivalence with reference", missing)
			}
		})
	}
}

func TestEquivalentResourceMapperDistinguishesEmptyAndMissingContext(t *testing.T) {
	t.Parallel()

	request := resourceReference("apps", "v1", "deployments", "")
	mapper := mustEquivalentResourceMapper(t, []contract.EquivalenceMapping{{
		Request:     request,
		Equivalents: []contract.EquivalentResource{},
	}})

	got, err := mapper.Lookup(request)
	if err != nil {
		t.Fatalf("Lookup(explicit empty) error = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("Lookup(explicit empty) = %#v, want non-nil empty slice", got)
	}

	got, err = mapper.Lookup(resourceReference("apps", "v1", "statefulsets", ""))
	if got != nil {
		t.Errorf("Lookup(missing) = %#v, want nil", got)
	}
	if !errors.Is(err, contract.ErrMissingContext) {
		t.Errorf("Lookup(missing) error = %v, want ErrMissingContext", err)
	}

	_, err = fixture.NewEquivalentResourceMapper([]contract.EquivalenceMapping{{Request: request}})
	assertInvalidEquivalence(t, err, ".externalContext.equivalence[0].equivalents")

	emptyMapper := mustEquivalentResourceMapper(t, nil)
	_, err = emptyMapper.Lookup(request)
	if !errors.Is(err, contract.ErrMissingContext) {
		t.Errorf("Lookup(nil mappings) error = %v, want ErrMissingContext", err)
	}
}

func TestNewEquivalentResourceMapperRejectsDuplicateAndConflictingMappings(t *testing.T) {
	t.Parallel()

	request := resourceReference("apps", "v1", "deployments", "")
	otherRequest := resourceReference("apps", "v1", "statefulsets", "")
	target := equivalentResource("apps", "v1beta1", "deployments", "apps", "v1beta1", "Deployment", "")
	conflictingTarget := target
	conflictingTarget.GVK.Kind = "DeploymentList"
	crossSubresourceTarget := target
	crossSubresourceTarget.Subresource = "status"
	tests := []struct {
		name      string
		mappings  []contract.EquivalenceMapping
		wantField string
	}{
		{
			name: "duplicate request key",
			mappings: []contract.EquivalenceMapping{
				{Request: request, Equivalents: []contract.EquivalentResource{}},
				{Request: request, Equivalents: []contract.EquivalentResource{}},
			},
			wantField: ".externalContext.equivalence[1].request",
		},
		{
			name: "duplicate target key",
			mappings: []contract.EquivalenceMapping{{
				Request:     request,
				Equivalents: []contract.EquivalentResource{target, target},
			}},
			wantField: ".externalContext.equivalence[0].equivalents[1]",
		},
		{
			name: "target conflict in one mapping",
			mappings: []contract.EquivalenceMapping{{
				Request:     request,
				Equivalents: []contract.EquivalentResource{target, conflictingTarget},
			}},
			wantField: ".externalContext.equivalence[0].equivalents[1].gvk",
		},
		{
			name: "target conflict across mappings",
			mappings: []contract.EquivalenceMapping{
				{Request: request, Equivalents: []contract.EquivalentResource{target}},
				{Request: otherRequest, Equivalents: []contract.EquivalentResource{conflictingTarget}},
			},
			wantField: ".externalContext.equivalence[1].equivalents[0].gvk",
		},
		{
			name: "duplicate remains invalid before capability check",
			mappings: []contract.EquivalenceMapping{{
				Request:     request,
				Equivalents: []contract.EquivalentResource{crossSubresourceTarget, crossSubresourceTarget},
			}},
			wantField: ".externalContext.equivalence[0].equivalents[1]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := fixture.NewEquivalentResourceMapper(test.mappings)
			assertInvalidEquivalence(t, err, test.wantField)
		})
	}
}

func TestNewEquivalentResourceMapperAllowsSharedTargetKind(t *testing.T) {
	t.Parallel()

	target := equivalentResource("apps", "v1beta1", "deployments", "apps", "v1beta1", "Deployment", "")
	mappings := []contract.EquivalenceMapping{
		{Request: resourceReference("apps", "v1", "deployments", ""), Equivalents: []contract.EquivalentResource{target}},
		{Request: resourceReference("extensions", "v1beta1", "deployments", ""), Equivalents: []contract.EquivalentResource{target}},
	}
	mapper := mustEquivalentResourceMapper(t, mappings)
	for _, mapping := range mappings {
		got, err := mapper.Lookup(mapping.Request)
		if err != nil {
			t.Fatalf("Lookup(%#v) error = %v", mapping.Request, err)
		}
		if !reflect.DeepEqual(got, []contract.EquivalentResource{target}) {
			t.Errorf("Lookup(%#v) = %#v, want shared target %#v", mapping.Request, got, target)
		}
	}
}

func TestNewEquivalentResourceMapperRejectsMalformedReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*contract.EquivalenceMapping)
		wantField string
	}{
		{
			name: "request group slash",
			mutate: func(mapping *contract.EquivalenceMapping) {
				mapping.Request.GVR.Group = "apps/example.io"
			},
			wantField: ".externalContext.equivalence[0].request.gvr.group",
		},
		{
			name: "request version empty",
			mutate: func(mapping *contract.EquivalenceMapping) {
				mapping.Request.GVR.Version = ""
			},
			wantField: ".externalContext.equivalence[0].request.gvr.version",
		},
		{
			name: "request resource empty",
			mutate: func(mapping *contract.EquivalenceMapping) {
				mapping.Request.GVR.Resource = ""
			},
			wantField: ".externalContext.equivalence[0].request.gvr.resource",
		},
		{
			name: "request resource includes subresource",
			mutate: func(mapping *contract.EquivalenceMapping) {
				mapping.Request.GVR.Resource = "deployments/status"
			},
			wantField: ".externalContext.equivalence[0].request.gvr.resource",
		},
		{
			name: "request subresource slash",
			mutate: func(mapping *contract.EquivalenceMapping) {
				mapping.Request.Subresource = "status/scale"
			},
			wantField: ".externalContext.equivalence[0].request.subresource",
		},
		{
			name: "target version empty",
			mutate: func(mapping *contract.EquivalenceMapping) {
				mapping.Equivalents[0].GVR.Version = ""
			},
			wantField: ".externalContext.equivalence[0].equivalents[0].gvr.version",
		},
		{
			name: "target resource whitespace",
			mutate: func(mapping *contract.EquivalenceMapping) {
				mapping.Equivalents[0].GVR.Resource = "deployments "
			},
			wantField: ".externalContext.equivalence[0].equivalents[0].gvr.resource",
		},
		{
			name: "target kind version empty",
			mutate: func(mapping *contract.EquivalenceMapping) {
				mapping.Equivalents[0].GVK.Version = ""
			},
			wantField: ".externalContext.equivalence[0].equivalents[0].gvk.version",
		},
		{
			name: "target kind empty",
			mutate: func(mapping *contract.EquivalenceMapping) {
				mapping.Equivalents[0].GVK.Kind = ""
			},
			wantField: ".externalContext.equivalence[0].equivalents[0].gvk.kind",
		},
		{
			name: "target subresource slash",
			mutate: func(mapping *contract.EquivalenceMapping) {
				mapping.Equivalents[0].Subresource = "status/scale"
			},
			wantField: ".externalContext.equivalence[0].equivalents[0].subresource",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			mapping := validEquivalenceMapping()
			test.mutate(&mapping)
			_, err := fixture.NewEquivalentResourceMapper([]contract.EquivalenceMapping{mapping})
			assertInvalidEquivalence(t, err, test.wantField)
		})
	}
}

func TestNewEquivalentResourceMapperSupportsCoreAndScaleKinds(t *testing.T) {
	t.Parallel()

	mappings := []contract.EquivalenceMapping{
		{
			Request: resourceReference("", "v1", "pods", ""),
			Equivalents: []contract.EquivalentResource{
				equivalentResource("", "v1", "pods", "", "v1", "Pod", ""),
			},
		},
		{
			Request: resourceReference("apps", "v1", "deployments", "scale"),
			Equivalents: []contract.EquivalentResource{
				equivalentResource("apps", "v1beta1", "deployments", "autoscaling", "v1", "Scale", "scale"),
			},
		},
	}
	mapper := mustEquivalentResourceMapper(t, mappings)
	for _, mapping := range mappings {
		got, err := mapper.Lookup(mapping.Request)
		if err != nil {
			t.Fatalf("Lookup(%#v) error = %v", mapping.Request, err)
		}
		if !reflect.DeepEqual(got, mapping.Equivalents) {
			t.Errorf("Lookup(%#v) = %#v, want %#v", mapping.Request, got, mapping.Equivalents)
		}
	}
}

func TestNewEquivalentResourceMapperRejectsCrossSubresourceConversion(t *testing.T) {
	t.Parallel()

	mapping := validEquivalenceMapping()
	mapping.Equivalents[0].Subresource = "scale"
	_, err := fixture.NewEquivalentResourceMapper([]contract.EquivalenceMapping{mapping})
	if !errors.Is(err, contract.ErrUnsupportedCapability) {
		t.Fatalf("NewEquivalentResourceMapper() error = %v, want ErrUnsupportedCapability", err)
	}
	if errors.Is(err, contract.ErrInvalidInput) || errors.Is(err, contract.ErrMissingContext) {
		t.Errorf("NewEquivalentResourceMapper() error = %v, want only unsupported category", err)
	}
	var unsupported *contract.UnsupportedCapabilityError
	if !errors.As(err, &unsupported) {
		t.Fatalf("NewEquivalentResourceMapper() error type = %T, want *contract.UnsupportedCapabilityError", err)
	}
	wantPath := ".externalContext.equivalence[0].equivalents[0].subresource"
	if !strings.Contains(unsupported.Capability, wantPath) {
		t.Errorf("UnsupportedCapabilityError.Capability = %q, want source path %q", unsupported.Capability, wantPath)
	}
}

func TestNewEquivalentResourceMapperReportsStructuralErrorsBeforeUnsupported(t *testing.T) {
	t.Parallel()

	mapping := validEquivalenceMapping()
	mapping.Equivalents[0].Subresource = "scale"
	mapping.Equivalents = append(mapping.Equivalents, mapping.Equivalents[0])

	_, err := fixture.NewEquivalentResourceMapper([]contract.EquivalenceMapping{mapping})
	assertInvalidEquivalence(t, err, ".externalContext.equivalence[0].equivalents[1]")
}

func mustEquivalentResourceMapper(t *testing.T, mappings []contract.EquivalenceMapping) fixture.EquivalentResourceMapper {
	t.Helper()

	mapper, err := fixture.NewEquivalentResourceMapper(mappings)
	if err != nil {
		t.Fatalf("NewEquivalentResourceMapper() error = %v", err)
	}
	return mapper
}

func assertInvalidEquivalence(t *testing.T, err error, wantField string) {
	t.Helper()

	if !errors.Is(err, contract.ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if errors.Is(err, contract.ErrMissingContext) || errors.Is(err, contract.ErrUnsupportedCapability) {
		t.Errorf("error = %v, want only invalid-input category", err)
	}
	var invalid *contract.InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("error type = %T, want *contract.InvalidInputError", err)
	}
	if invalid.Field != wantField {
		t.Errorf("InvalidInputError.Field = %q, want %q", invalid.Field, wantField)
	}
}

func validEquivalenceMapping() contract.EquivalenceMapping {
	return contract.EquivalenceMapping{
		Request: resourceReference("apps", "v1", "deployments", "status"),
		Equivalents: []contract.EquivalentResource{
			equivalentResource("apps", "v1beta1", "deployments", "apps", "v1beta1", "Deployment", "status"),
		},
	}
}

func resourceReference(group, version, resource, subresource string) contract.ResourceReference {
	return contract.ResourceReference{
		GVR: metav1.GroupVersionResource{
			Group:    group,
			Version:  version,
			Resource: resource,
		},
		Subresource: subresource,
	}
}

func equivalentResource(
	resourceGroup, resourceVersion, resource string,
	kindGroup, kindVersion, kind, subresource string,
) contract.EquivalentResource {
	return contract.EquivalentResource{
		GVR: metav1.GroupVersionResource{
			Group:    resourceGroup,
			Version:  resourceVersion,
			Resource: resource,
		},
		GVK: metav1.GroupVersionKind{
			Group:   kindGroup,
			Version: kindVersion,
			Kind:    kind,
		},
		Subresource: subresource,
	}
}
