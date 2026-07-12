package normalize

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/silbaram/admitrace/internal/contract"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// ObjectSnapshotState distinguishes an absent object, an explicit JSON null,
// and an object payload.
type ObjectSnapshotState string

const (
	// ObjectSnapshotAbsent indicates that the request field was omitted.
	ObjectSnapshotAbsent ObjectSnapshotState = "absent"
	// ObjectSnapshotNull indicates that the request field contained JSON null.
	ObjectSnapshotNull ObjectSnapshotState = "null"
	// ObjectSnapshotObject indicates that the request field contained a JSON object.
	ObjectSnapshotObject ObjectSnapshotState = "object"
)

// ObjectSnapshot is an owned raw request object plus routing-relevant metadata.
type ObjectSnapshot struct {
	State       ObjectSnapshotState
	Raw         json.RawMessage
	APIVersion  string
	Kind        string
	GVK         schema.GroupVersionKind
	Name        string
	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
}

// RequestContext is the deterministic request input consumed by later routing
// and CEL evaluation stages.
type RequestContext struct {
	Operation          admissionv1.Operation
	Kind               schema.GroupVersionKind
	Resource           schema.GroupVersionResource
	Subresource        string
	RequestKind        schema.GroupVersionKind
	RequestResource    schema.GroupVersionResource
	RequestSubresource string
	Scope              contract.RequestScope
	Namespace          string
	Name               string
	DryRunPresent      bool
	DryRun             bool
	UID                types.UID
	UserInfo           authenticationv1.UserInfo
	Object             ObjectSnapshot
	OldObject          ObjectSnapshot
}

// BuildRequestContext derives routing and object metadata without retaining
// mutable data owned by the input AdmissionRequest.
func BuildRequestContext(request contract.AdmissionRequest) (RequestContext, error) {
	object, err := parseObjectSnapshot(request.Object, ".request.object")
	if err != nil {
		return RequestContext{}, err
	}
	oldObject, err := parseObjectSnapshot(request.OldObject, ".request.oldObject")
	if err != nil {
		return RequestContext{}, err
	}

	kind := groupVersionKind(request.Kind)
	requestKind := kind
	if request.RequestKind != nil {
		requestKind = groupVersionKind(*request.RequestKind)
	}
	resource := groupVersionResource(request.Resource)
	requestResource := resource
	if request.RequestResource != nil {
		requestResource = groupVersionResource(*request.RequestResource)
	}
	requestSubresource := request.RequestSubResource
	if request.RequestResource == nil {
		requestSubresource = request.SubResource
	}

	context := RequestContext{
		Operation:          request.Operation,
		Kind:               kind,
		Resource:           resource,
		Subresource:        request.SubResource,
		RequestKind:        requestKind,
		RequestResource:    requestResource,
		RequestSubresource: requestSubresource,
		Scope:              request.Scope,
		Namespace:          request.Namespace,
		Name:               request.Name,
		UID:                request.UID,
		UserInfo:           cloneUserInfo(request.UserInfo),
		Object:             object,
		OldObject:          oldObject,
	}
	if request.DryRun != nil {
		context.DryRunPresent = true
		context.DryRun = *request.DryRun
	}
	return context, nil
}

type objectDocument struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   objectMetadata `json:"metadata"`
}

type objectMetadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

func parseObjectSnapshot(input json.RawMessage, path string) (ObjectSnapshot, error) {
	if input == nil {
		return ObjectSnapshot{State: ObjectSnapshotAbsent}, nil
	}

	raw := cloneSlice(input)
	trimmed := bytes.TrimSpace(raw)
	if !json.Valid(trimmed) {
		return ObjectSnapshot{}, invalidInputCause(path, errors.New("object snapshot contains malformed JSON"))
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return ObjectSnapshot{State: ObjectSnapshotNull, Raw: raw}, nil
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ObjectSnapshot{}, invalidInputCause(path, errors.New("object snapshot must be a JSON object or null"))
	}

	var document objectDocument
	if err := json.Unmarshal(trimmed, &document); err != nil {
		return ObjectSnapshot{}, invalidInputCause(path, fmt.Errorf("object snapshot metadata is malformed: %w", err))
	}
	return ObjectSnapshot{
		State:       ObjectSnapshotObject,
		Raw:         raw,
		APIVersion:  document.APIVersion,
		Kind:        document.Kind,
		GVK:         schema.FromAPIVersionAndKind(document.APIVersion, document.Kind),
		Name:        document.Metadata.Name,
		Namespace:   document.Metadata.Namespace,
		Labels:      cloneStringMap(document.Metadata.Labels),
		Annotations: cloneStringMap(document.Metadata.Annotations),
	}, nil
}

func groupVersionKind(input metav1.GroupVersionKind) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   input.Group,
		Version: input.Version,
		Kind:    input.Kind,
	}
}

func groupVersionResource(input metav1.GroupVersionResource) schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    input.Group,
		Version:  input.Version,
		Resource: input.Resource,
	}
}

func cloneUserInfo(input authenticationv1.UserInfo) authenticationv1.UserInfo {
	result := input
	result.Groups = cloneSlice(input.Groups)
	if input.Extra == nil {
		return result
	}
	result.Extra = make(map[string]authenticationv1.ExtraValue, len(input.Extra))
	for key, values := range input.Extra {
		result.Extra[key] = cloneSlice(values)
	}
	return result
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func invalidInputCause(field string, cause error) error {
	return &contract.InvalidInputError{Field: field, Err: cause}
}
