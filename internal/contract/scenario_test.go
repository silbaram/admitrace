package contract_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
)

func TestWebhookConfigurationKind(t *testing.T) {
	t.Parallel()

	validating := &kube136.ValidatingWebhookConfiguration{}
	mutating := &kube136.MutatingWebhookConfiguration{}
	tests := []struct {
		name          string
		configuration contract.WebhookConfiguration
		wantKind      contract.ConfigurationKind
		wantOK        bool
	}{
		{
			name:          "validating",
			configuration: contract.WebhookConfiguration{Validating: validating},
			wantKind:      contract.ConfigurationKindValidating,
			wantOK:        true,
		},
		{
			name:          "mutating",
			configuration: contract.WebhookConfiguration{Mutating: mutating},
			wantKind:      contract.ConfigurationKindMutating,
			wantOK:        true,
		},
		{name: "missing"},
		{
			name:          "both",
			configuration: contract.WebhookConfiguration{Validating: validating, Mutating: mutating},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			gotKind, gotOK := test.configuration.Kind()
			if gotKind != test.wantKind || gotOK != test.wantOK {
				t.Errorf("Kind() = (%q, %t), want (%q, %t)", gotKind, gotOK, test.wantKind, test.wantOK)
			}
			if got := test.configuration.HasExactlyOne(); got != test.wantOK {
				t.Errorf("HasExactlyOne() = %t, want %t", got, test.wantOK)
			}
		})
	}
}

func TestAdmissionRequestPreservesRawPayloads(t *testing.T) {
	t.Parallel()

	const object = `{"apiVersion":"example.io/v1","kind":"Widget","spec":{"extension":{"enabled":true,"weight":1.25}},"unknown":[null,"value"]}`
	const input = `{"kind":{"group":"example.io","version":"v1","kind":"Widget"},"resource":{"group":"example.io","version":"v1","resource":"widgets"},"operation":"UPDATE","scope":"Namespaced","userInfo":{"username":"alice"},"object":` + object + `,"oldObject":null}`

	var request contract.AdmissionRequest
	if err := json.Unmarshal([]byte(input), &request); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := string(request.Object); got != object {
		t.Errorf("Object = %s, want %s", got, object)
	}
	if got := string(request.OldObject); got != "null" {
		t.Errorf("OldObject = %q, want %q", got, "null")
	}
	roundTripJSON, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal() round trip error = %v", err)
	}
	var roundTrip contract.AdmissionRequest
	if err := json.Unmarshal(roundTripJSON, &roundTrip); err != nil {
		t.Fatalf("json.Unmarshal() round trip error = %v", err)
	}
	if !bytes.Equal(roundTrip.Object, request.Object) || !bytes.Equal(roundTrip.OldObject, request.OldObject) {
		t.Errorf("round-trip payloads = (%s, %s), want (%s, %s)", roundTrip.Object, roundTrip.OldObject, request.Object, request.OldObject)
	}

	var absent contract.AdmissionRequest
	if err := json.Unmarshal([]byte(`{"kind":{},"resource":{},"operation":"CREATE","scope":"Cluster","userInfo":{}}`), &absent); err != nil {
		t.Fatalf("json.Unmarshal() absent payload error = %v", err)
	}
	if absent.Object != nil || absent.OldObject != nil {
		t.Errorf("absent payloads = (%s, %s), want both nil", absent.Object, absent.OldObject)
	}
	body, err := json.Marshal(absent)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(body), `"object"`) || strings.Contains(string(body), `"oldObject"`) {
		t.Errorf("json.Marshal() = %s, want absent raw payload fields omitted", body)
	}
}

func TestAuthorizationQueryHasExactlyOne(t *testing.T) {
	t.Parallel()

	resource := &contract.ResourceAuthorizationQuery{Verb: "get", Resource: "pods"}
	nonResource := &contract.NonResourceAuthorizationQuery{Verb: "get", Path: "/healthz"}
	tests := []struct {
		name  string
		query contract.AuthorizationQuery
		want  bool
	}{
		{name: "resource", query: contract.AuthorizationQuery{Resource: resource}, want: true},
		{name: "non-resource", query: contract.AuthorizationQuery{NonResource: nonResource}, want: true},
		{name: "missing", query: contract.AuthorizationQuery{}, want: false},
		{name: "both", query: contract.AuthorizationQuery{Resource: resource, NonResource: nonResource}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := test.query.HasExactlyOne(); got != test.want {
				t.Errorf("HasExactlyOne() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestExplicitNoOpinionDiffersFromMissingAuthorizationFixture(t *testing.T) {
	t.Parallel()

	missing, err := json.Marshal(contract.ExternalContext{})
	if err != nil {
		t.Fatalf("json.Marshal() missing fixture error = %v", err)
	}
	explicit, err := json.Marshal(contract.ExternalContext{
		Authorization: []contract.AuthorizationDecision{
			{
				Query: contract.AuthorizationQuery{
					NonResource: &contract.NonResourceAuthorizationQuery{Verb: "get", Path: "/healthz"},
				},
				Verdict: contract.AuthorizationVerdictNoOpinion,
			},
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() explicit fixture error = %v", err)
	}

	if strings.Contains(string(missing), `"authorization"`) {
		t.Errorf("missing fixture JSON = %s, want authorization omitted", missing)
	}
	if !strings.Contains(string(explicit), `"verdict":"no-opinion"`) {
		t.Errorf("explicit fixture JSON = %s, want explicit no-opinion verdict", explicit)
	}
}
