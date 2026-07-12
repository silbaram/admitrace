package normalize_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/normalize"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func TestBuildRequestContextDerivesRoutingAndObjectMetadata(t *testing.T) {
	t.Parallel()

	dryRun := false
	object := json.RawMessage(` {"apiVersion":"apps.example.io/v1","kind":"Widget","metadata":{"name":"new-widget","namespace":"widgets","labels":{"environment":"production"},"annotations":{"owner":"platform"}},"spec":{"ignored":true}} `)
	oldObject := json.RawMessage(`{"apiVersion":"apps.example.io/v1beta1","kind":"Widget","metadata":{"name":"old-widget","namespace":"legacy","labels":{"environment":"staging"},"annotations":{"owner":"legacy"}}}`)
	request := contract.AdmissionRequest{
		UID:         types.UID("request-123"),
		Kind:        metav1.GroupVersionKind{Group: "apps.example.io", Version: "v1", Kind: "Widget"},
		Resource:    metav1.GroupVersionResource{Group: "apps.example.io", Version: "v1", Resource: "widgets"},
		SubResource: "status",
		Name:        "new-widget",
		Namespace:   "widgets",
		Operation:   admissionv1.Update,
		Scope:       contract.RequestScopeNamespaced,
		UserInfo: authenticationv1.UserInfo{
			Username: "alice",
			UID:      "alice-uid",
			Groups:   []string{"developers", "system:authenticated"},
			Extra: map[string]authenticationv1.ExtraValue{
				"scopes": {"write", "read"},
			},
		},
		Object:    object,
		OldObject: oldObject,
		DryRun:    &dryRun,
	}

	got, err := normalize.BuildRequestContext(request)
	if err != nil {
		t.Fatalf("BuildRequestContext() error = %v", err)
	}
	want := normalize.RequestContext{
		Operation:          admissionv1.Update,
		Kind:               schema.GroupVersionKind{Group: "apps.example.io", Version: "v1", Kind: "Widget"},
		Resource:           schema.GroupVersionResource{Group: "apps.example.io", Version: "v1", Resource: "widgets"},
		Subresource:        "status",
		RequestKind:        schema.GroupVersionKind{Group: "apps.example.io", Version: "v1", Kind: "Widget"},
		RequestResource:    schema.GroupVersionResource{Group: "apps.example.io", Version: "v1", Resource: "widgets"},
		RequestSubresource: "status",
		Scope:              contract.RequestScopeNamespaced,
		Namespace:          "widgets",
		Name:               "new-widget",
		DryRunPresent:      true,
		DryRun:             false,
		UID:                types.UID("request-123"),
		UserInfo: authenticationv1.UserInfo{
			Username: "alice",
			UID:      "alice-uid",
			Groups:   []string{"developers", "system:authenticated"},
			Extra: map[string]authenticationv1.ExtraValue{
				"scopes": {"write", "read"},
			},
		},
		Object: normalize.ObjectSnapshot{
			State:       normalize.ObjectSnapshotObject,
			Raw:         object,
			APIVersion:  "apps.example.io/v1",
			Kind:        "Widget",
			GVK:         schema.GroupVersionKind{Group: "apps.example.io", Version: "v1", Kind: "Widget"},
			Name:        "new-widget",
			Namespace:   "widgets",
			Labels:      map[string]string{"environment": "production"},
			Annotations: map[string]string{"owner": "platform"},
		},
		OldObject: normalize.ObjectSnapshot{
			State:       normalize.ObjectSnapshotObject,
			Raw:         oldObject,
			APIVersion:  "apps.example.io/v1beta1",
			Kind:        "Widget",
			GVK:         schema.GroupVersionKind{Group: "apps.example.io", Version: "v1beta1", Kind: "Widget"},
			Name:        "old-widget",
			Namespace:   "legacy",
			Labels:      map[string]string{"environment": "staging"},
			Annotations: map[string]string{"owner": "legacy"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildRequestContext() = %#v, want %#v", got, want)
	}
}

func TestBuildRequestContextUsesExplicitOriginalRequestOverrides(t *testing.T) {
	t.Parallel()

	requestKind := metav1.GroupVersionKind{Group: "extensions", Version: "v1beta1", Kind: "Deployment"}
	requestResource := metav1.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "deployments"}
	request := contract.AdmissionRequest{
		Kind:               metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		Resource:           metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		SubResource:        "scale",
		RequestKind:        &requestKind,
		RequestResource:    &requestResource,
		RequestSubResource: "rollback",
	}

	got, err := normalize.BuildRequestContext(request)
	if err != nil {
		t.Fatalf("BuildRequestContext() error = %v", err)
	}
	if got.RequestKind != (schema.GroupVersionKind{Group: "extensions", Version: "v1beta1", Kind: "Deployment"}) {
		t.Errorf("RequestKind = %#v, want explicit override", got.RequestKind)
	}
	if got.RequestResource != (schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "deployments"}) {
		t.Errorf("RequestResource = %#v, want explicit override", got.RequestResource)
	}
	if got.RequestSubresource != "rollback" {
		t.Errorf("RequestSubresource = %q, want explicit override rollback", got.RequestSubresource)
	}

	request.RequestSubResource = ""
	got, err = normalize.BuildRequestContext(request)
	if err != nil {
		t.Fatalf("BuildRequestContext(explicit resource without subresource) error = %v", err)
	}
	if got.RequestSubresource != "" {
		t.Errorf("RequestSubresource = %q, want preserved explicit empty value", got.RequestSubresource)
	}
}

func TestBuildRequestContextDistinguishesDryRunPresence(t *testing.T) {
	t.Parallel()

	falseValue := false
	trueValue := true
	tests := []struct {
		name        string
		dryRun      *bool
		wantPresent bool
		wantValue   bool
	}{
		{name: "absent", dryRun: nil, wantPresent: false, wantValue: false},
		{name: "explicit false", dryRun: &falseValue, wantPresent: true, wantValue: false},
		{name: "explicit true", dryRun: &trueValue, wantPresent: true, wantValue: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalize.BuildRequestContext(contract.AdmissionRequest{DryRun: test.dryRun})
			if err != nil {
				t.Fatalf("BuildRequestContext() error = %v", err)
			}
			if got.DryRunPresent != test.wantPresent || got.DryRun != test.wantValue {
				t.Errorf("dryRun = (present %t, value %t), want (present %t, value %t)", got.DryRunPresent, got.DryRun, test.wantPresent, test.wantValue)
			}
		})
	}
}

func TestBuildRequestContextDistinguishesObjectSnapshotStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		object    json.RawMessage
		wantState normalize.ObjectSnapshotState
		wantRaw   json.RawMessage
	}{
		{name: "absent", object: nil, wantState: normalize.ObjectSnapshotAbsent, wantRaw: nil},
		{name: "explicit null", object: json.RawMessage("  null\n"), wantState: normalize.ObjectSnapshotNull, wantRaw: json.RawMessage("  null\n")},
		{name: "object", object: json.RawMessage("{}"), wantState: normalize.ObjectSnapshotObject, wantRaw: json.RawMessage("{}")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalize.BuildRequestContext(contract.AdmissionRequest{Object: test.object, OldObject: test.object})
			if err != nil {
				t.Fatalf("BuildRequestContext() error = %v", err)
			}
			if got.Object.State != test.wantState {
				t.Errorf("Object.State = %q, want %q", got.Object.State, test.wantState)
			}
			if !bytes.Equal(got.Object.Raw, test.wantRaw) || (got.Object.Raw == nil) != (test.wantRaw == nil) {
				t.Errorf("Object.Raw = %q (nil %t), want %q (nil %t)", got.Object.Raw, got.Object.Raw == nil, test.wantRaw, test.wantRaw == nil)
			}
			if got.OldObject.State != test.wantState {
				t.Errorf("OldObject.State = %q, want %q", got.OldObject.State, test.wantState)
			}
			if !bytes.Equal(got.OldObject.Raw, test.wantRaw) || (got.OldObject.Raw == nil) != (test.wantRaw == nil) {
				t.Errorf("OldObject.Raw = %q (nil %t), want %q (nil %t)", got.OldObject.Raw, got.OldObject.Raw == nil, test.wantRaw, test.wantRaw == nil)
			}
		})
	}
}

func TestBuildRequestContextDeepCopiesInput(t *testing.T) {
	t.Parallel()

	dryRun := false
	requestKind := metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	requestResource := metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	object := json.RawMessage(`{"metadata":{"labels":{"environment":"production"},"annotations":{"owner":"platform"}}}`)
	request := contract.AdmissionRequest{
		RequestKind:     &requestKind,
		RequestResource: &requestResource,
		UserInfo: authenticationv1.UserInfo{
			Groups: []string{"developers"},
			Extra:  map[string]authenticationv1.ExtraValue{"scopes": {"write"}},
		},
		Object: object,
		DryRun: &dryRun,
	}
	originalObject := append(json.RawMessage(nil), object...)
	got, err := normalize.BuildRequestContext(request)
	if err != nil {
		t.Fatalf("BuildRequestContext() error = %v", err)
	}

	request.Object[0] = '['
	request.UserInfo.Groups[0] = "changed"
	request.UserInfo.Extra["scopes"][0] = "changed"
	request.RequestKind.Group = "changed"
	request.RequestResource.Group = "changed"
	*request.DryRun = true
	if !bytes.Equal(got.Object.Raw, originalObject) {
		t.Errorf("Object.Raw changed with input = %q, want %q", got.Object.Raw, originalObject)
	}
	if got.UserInfo.Groups[0] != "developers" || got.UserInfo.Extra["scopes"][0] != "write" {
		t.Errorf("UserInfo changed with input = %#v, want original values", got.UserInfo)
	}
	if got.RequestKind.Group != "apps" || got.RequestResource.Group != "apps" || got.DryRun {
		t.Errorf("scalar context changed with input = kind %#v, resource %#v, dryRun %t", got.RequestKind, got.RequestResource, got.DryRun)
	}

	got.Object.Raw[1] = ' '
	got.Object.Labels["environment"] = "changed"
	got.Object.Annotations["owner"] = "changed"
	got.UserInfo.Groups[0] = "context-change"
	got.UserInfo.Extra["scopes"][0] = "context-change"
	if request.UserInfo.Groups[0] != "changed" || request.UserInfo.Extra["scopes"][0] != "changed" {
		t.Errorf("input UserInfo changed with context = %#v", request.UserInfo)
	}
}

func TestBuildRequestContextRejectsInvalidObjectSnapshots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		object   json.RawMessage
		old      json.RawMessage
		wantPath string
	}{
		{name: "empty", object: json.RawMessage{}, wantPath: ".request.object"},
		{name: "malformed", object: json.RawMessage(`{"metadata":`), wantPath: ".request.object"},
		{name: "number", object: json.RawMessage("1"), wantPath: ".request.object"},
		{name: "string", object: json.RawMessage(`"sensitive-object-name"`), wantPath: ".request.object"},
		{name: "boolean", object: json.RawMessage("true"), wantPath: ".request.object"},
		{name: "array", object: json.RawMessage(`[{"metadata":{"name":"sensitive-object-name"}}]`), wantPath: ".request.object"},
		{name: "invalid metadata", object: json.RawMessage(`{"metadata":{"name":"sensitive-object-name","labels":{"key":1}}}`), wantPath: ".request.object"},
		{name: "old object malformed", object: json.RawMessage("{}"), old: json.RawMessage("["), wantPath: ".request.oldObject"},
		{name: "old object scalar", object: json.RawMessage("{}"), old: json.RawMessage(`"sensitive-old-name"`), wantPath: ".request.oldObject"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalize.BuildRequestContext(contract.AdmissionRequest{Object: test.object, OldObject: test.old})
			requireInvalidInput(t, err, test.wantPath)
			if strings.Contains(err.Error(), "sensitive-object-name") || strings.Contains(err.Error(), "sensitive-old-name") {
				t.Errorf("error exposes object identity: %v", err)
			}
		})
	}
}

func TestNormalizationIsDeterministicAcrossRepeatedCalls(t *testing.T) {
	t.Parallel()

	configuration := contract.WebhookConfiguration{Validating: newValidatingConfiguration("first.policy.example.com", "second.policy.example.com")}
	firstWebhooks, err := normalize.Webhooks(configuration)
	if err != nil {
		t.Fatalf("first Webhooks() error = %v", err)
	}
	secondWebhooks, err := normalize.Webhooks(configuration)
	if err != nil {
		t.Fatalf("second Webhooks() error = %v", err)
	}
	if !reflect.DeepEqual(firstWebhooks, secondWebhooks) {
		t.Errorf("repeated Webhooks() differ: first %#v, second %#v", firstWebhooks, secondWebhooks)
	}

	request := contract.AdmissionRequest{
		UserInfo: authenticationv1.UserInfo{Extra: map[string]authenticationv1.ExtraValue{"z": {"last"}, "a": {"first"}}},
		Object:   json.RawMessage(`{"metadata":{"labels":{"z":"last","a":"first"}}}`),
	}
	firstContext, err := normalize.BuildRequestContext(request)
	if err != nil {
		t.Fatalf("first BuildRequestContext() error = %v", err)
	}
	secondContext, err := normalize.BuildRequestContext(request)
	if err != nil {
		t.Fatalf("second BuildRequestContext() error = %v", err)
	}
	if !reflect.DeepEqual(firstContext, secondContext) {
		t.Errorf("repeated BuildRequestContext() differ: first %#v, second %#v", firstContext, secondContext)
	}
}
