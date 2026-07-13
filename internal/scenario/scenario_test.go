package scenario_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/scenario"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDecodeValidYAMLAndJSON(t *testing.T) {
	t.Parallel()

	jsonInput := newScenario(contract.ConfigurationKindMutating)
	jsonInput.Configuration.Mutating.Webhooks = append(
		jsonInput.Configuration.Mutating.Webhooks,
		validMutatingWebhook("second.policy.example.com"),
	)
	jsonData, err := json.Marshal(jsonInput)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	const yamlData = `apiVersion: admitrace.io/v1alpha1
kind: Scenario
metadata:
  name: yaml-example
compatibilityProfile:
  id: kubernetes-1.36.2-defaults
  kubernetesVersion: 1.36.2
  featureGatePolicy: kubernetes-defaults
configuration:
  validatingWebhookConfiguration:
    apiVersion: admissionregistration.k8s.io/v1
    kind: ValidatingWebhookConfiguration
    webhooks:
      - name: first.policy.example.com
        rules:
          - operations: [CREATE]
            apiGroups: [""]
            apiVersions: [v1]
            resources: [pods]
request:
  kind:
    version: v1
    kind: Pod
  resource:
    version: v1
    resource: pods
  operation: CREATE
  scope: Namespaced
  userInfo:
    username: alice
`

	tests := []struct {
		name      string
		data      []byte
		wantKind  contract.ConfigurationKind
		wantNames []string
		wantName  string
	}{
		{
			name:      "json",
			data:      jsonData,
			wantKind:  contract.ConfigurationKindMutating,
			wantNames: []string{"first.policy.example.com", "second.policy.example.com"},
			wantName:  "routing-example",
		},
		{
			name:      "yaml",
			data:      []byte(yamlData),
			wantKind:  contract.ConfigurationKindValidating,
			wantNames: []string{"first.policy.example.com"},
			wantName:  "yaml-example",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := scenario.Decode(test.data)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got.Metadata.Name != test.wantName {
				t.Errorf("Metadata.Name = %q, want %q", got.Metadata.Name, test.wantName)
			}
			gotKind, ok := got.Configuration.Kind()
			if !ok || gotKind != test.wantKind {
				t.Fatalf("Configuration.Kind() = (%q, %t), want (%q, true)", gotKind, ok, test.wantKind)
			}
			gotNames := webhookNames(got)
			if !slices.Equal(gotNames, test.wantNames) {
				t.Errorf("webhook names = %v, want %v", gotNames, test.wantNames)
			}
		})
	}
}

func TestDecodeRejectsStrictFieldViolations(t *testing.T) {
	t.Parallel()

	validConfiguration := validatingConfigurationDocument("", "")
	tests := []struct {
		name     string
		input    string
		wantPath string
	}{
		{
			name:     "unknown root field",
			input:    scenarioDocument(validConfiguration, "", `,"unexpected":true`),
			wantPath: ".unexpected",
		},
		{
			name:     "unknown configuration field",
			input:    scenarioDocument(validatingConfigurationDocument(`,"unexpected":true`, ""), "", ""),
			wantPath: ".configuration.validatingWebhookConfiguration.unexpected",
		},
		{
			name:     "unknown webhook field",
			input:    scenarioDocument(validatingConfigurationDocument("", `,"unexpected":true`), "", ""),
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].unexpected",
		},
		{
			name:     "unknown request envelope field",
			input:    scenarioDocument(validConfiguration, `,"unexpected":true`, ""),
			wantPath: ".request.unexpected",
		},
		{
			name:     "duplicate JSON root field",
			input:    scenarioDocument(validConfiguration, "", `,"kind":"Scenario"`),
			wantPath: ".kind",
		},
		{
			name:     "duplicate JSON webhook field",
			input:    scenarioDocument(validatingConfigurationDocument("", `,"name":"first.policy.example.com"`), "", ""),
			wantPath: ".configuration.validatingWebhookConfiguration.webhooks[0].name",
		},
		{
			name: "duplicate YAML key",
			input: `apiVersion: admitrace.io/v1alpha1
kind: Scenario
kind: Scenario
`,
			wantPath: ".",
		},
		{
			name: "invalid YAML",
			input: `apiVersion: admitrace.io/v1alpha1
kind: [
`,
			wantPath: ".",
		},
		{
			name:     "invalid JSON",
			input:    `{"apiVersion":`,
			wantPath: ".",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := scenario.Decode([]byte(test.input))
			requireInvalidInput(t, err, test.wantPath)
		})
	}
}

func TestDecodeValidatesEnvelopeAndConfigurationIdentity(t *testing.T) {
	t.Parallel()

	validating := validatingConfigurationDocument("", "")
	mutating := mutatingConfigurationDocument("", "")
	base := scenarioDocument(validating, "", "")
	tests := []struct {
		name     string
		input    string
		wantPath string
	}{
		{
			name:     "scenario apiVersion",
			input:    strings.Replace(base, contract.ScenarioAPIVersion, "admitrace.io/v2", 1),
			wantPath: ".apiVersion",
		},
		{
			name:     "scenario kind",
			input:    strings.Replace(base, `"kind":"Scenario"`, `"kind":"ScenarioList"`, 1),
			wantPath: ".kind",
		},
		{
			name:     "scenario metadata name",
			input:    strings.Replace(base, `"name":"decoder-example"`, `"name":""`, 1),
			wantPath: ".metadata.name",
		},
		{
			name:     "profile id",
			input:    strings.Replace(base, contract.Kubernetes136DefaultProfileID, "other-profile", 1),
			wantPath: ".compatibilityProfile.id",
		},
		{
			name:     "profile Kubernetes version",
			input:    strings.Replace(base, `"kubernetesVersion":"`+kube136.KubernetesVersion+`"`, `"kubernetesVersion":"1.35.0"`, 1),
			wantPath: ".compatibilityProfile.kubernetesVersion",
		},
		{
			name:     "profile feature gates",
			input:    strings.Replace(base, string(contract.FeatureGatePolicyKubernetesDefaults), "custom", 1),
			wantPath: ".compatibilityProfile.featureGatePolicy",
		},
		{
			name:     "configuration missing",
			input:    scenarioDocument(`{}`, "", ""),
			wantPath: ".configuration",
		},
		{
			name:     "both configurations",
			input:    scenarioDocument(`{"validatingWebhookConfiguration":`+validatingConfigurationValue("", "")+`,"mutatingWebhookConfiguration":`+mutatingConfigurationValue("", "")+`}`, "", ""),
			wantPath: ".configuration",
		},
		{
			name:     "validating configuration apiVersion",
			input:    strings.Replace(base, kube136.AdmissionRegistrationAPIVersion, "admissionregistration.k8s.io/v1beta1", 1),
			wantPath: ".configuration.validatingWebhookConfiguration.apiVersion",
		},
		{
			name:     "validating configuration kind",
			input:    strings.Replace(base, `"kind":"ValidatingWebhookConfiguration"`, `"kind":"MutatingWebhookConfiguration"`, 1),
			wantPath: ".configuration.validatingWebhookConfiguration.kind",
		},
		{
			name:     "mutating configuration apiVersion",
			input:    strings.Replace(scenarioDocument(mutating, "", ""), kube136.AdmissionRegistrationAPIVersion, "admissionregistration.k8s.io/v1beta1", 1),
			wantPath: ".configuration.mutatingWebhookConfiguration.apiVersion",
		},
		{
			name:     "mutating configuration kind",
			input:    strings.Replace(scenarioDocument(mutating, "", ""), `"kind":"MutatingWebhookConfiguration"`, `"kind":"ValidatingWebhookConfiguration"`, 1),
			wantPath: ".configuration.mutatingWebhookConfiguration.kind",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := scenario.Decode([]byte(test.input))
			requireInvalidInput(t, err, test.wantPath)
		})
	}
}

func TestDecodeValidatesExpectations(t *testing.T) {
	t.Parallel()

	called := contract.OutcomeCalled
	tests := []struct {
		name         string
		expectations []contract.WebhookExpectation
		wantPath     string
	}{
		{
			name: "valid optional assertions",
			expectations: []contract.WebhookExpectation{{
				WebhookName:        "first.policy.example.com",
				Determination:      contract.DeterminationDeterminate,
				Outcome:            &called,
				TerminalReasonCode: contract.ReasonCodeMatchConditionsTrue,
			}},
		},
		{
			name: "empty webhook name",
			expectations: []contract.WebhookExpectation{{
				Determination: contract.DeterminationDeterminate,
			}},
			wantPath: ".expectations[0].webhookName",
		},
		{
			name: "unknown webhook",
			expectations: []contract.WebhookExpectation{{
				WebhookName:   "unknown.policy.example.com",
				Determination: contract.DeterminationDeterminate,
			}},
			wantPath: ".expectations[0].webhookName",
		},
		{
			name: "duplicate webhook",
			expectations: []contract.WebhookExpectation{
				{WebhookName: "first.policy.example.com", Determination: contract.DeterminationDeterminate},
				{WebhookName: "first.policy.example.com", Determination: contract.DeterminationDeterminate},
			},
			wantPath: ".expectations[1].webhookName",
		},
		{
			name: "invalid determination",
			expectations: []contract.WebhookExpectation{{
				WebhookName:   "first.policy.example.com",
				Determination: "unknown",
			}},
			wantPath: ".expectations[0]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := newScenario(contract.ConfigurationKindValidating)
			input.Expectations = test.expectations
			data, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			_, err = scenario.Decode(data)
			if test.wantPath == "" {
				if err != nil {
					t.Fatalf("Decode() error = %v", err)
				}
				return
			}
			requireInvalidInput(t, err, test.wantPath)
		})
	}
}

func TestDecodePreservesRawRequestPayloadBoundaries(t *testing.T) {
	t.Parallel()

	const object = `{"apiVersion": "example.io/v1", "kind":"Widget","spec":{"extension":{"enabled":true,"weight":1.25}},"unknown":[null,"value"]}`
	const options = `{"apiVersion":"meta.k8s.io/v1","kind":"CreateOptions","custom":{"nested":true}}`
	input := scenarioDocument(
		validatingConfigurationDocument("", ""),
		`,"object":`+object+`,"oldObject":null,"options":`+options,
		"",
	)

	got, err := scenario.Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !bytes.Equal(got.Request.Object, []byte(object)) {
		t.Errorf("Request.Object = %s, want exact %s", got.Request.Object, object)
	}
	if !bytes.Equal(got.Request.OldObject, []byte("null")) {
		t.Errorf("Request.OldObject = %q, want %q", got.Request.OldObject, "null")
	}
	if !bytes.Equal(got.Request.Options, []byte(options)) {
		t.Errorf("Request.Options = %s, want exact %s", got.Request.Options, options)
	}
	explicitNull, err := scenario.Decode([]byte(scenarioDocument(
		validatingConfigurationDocument("", ""),
		`,"object":null,"oldObject":null,"options":null`,
		"",
	)))
	if err != nil {
		t.Fatalf("Decode() explicit null payload error = %v", err)
	}
	for _, payload := range []struct {
		name string
		data json.RawMessage
	}{
		{name: "object", data: explicitNull.Request.Object},
		{name: "oldObject", data: explicitNull.Request.OldObject},
		{name: "options", data: explicitNull.Request.Options},
	} {
		if !bytes.Equal(payload.data, []byte("null")) {
			t.Errorf("explicit null %s = %q, want %q", payload.name, payload.data, "null")
		}
	}

	absent, err := scenario.Decode([]byte(scenarioDocument(validatingConfigurationDocument("", ""), "", "")))
	if err != nil {
		t.Fatalf("Decode() absent payload error = %v", err)
	}
	if absent.Request.Object != nil || absent.Request.OldObject != nil || absent.Request.Options != nil {
		t.Errorf("absent raw payloads = (%s, %s, %s), want all nil", absent.Request.Object, absent.Request.OldObject, absent.Request.Options)
	}
}

func TestDecodePreservesYAMLRequestPayloadBoundaries(t *testing.T) {
	t.Parallel()

	const input = `apiVersion: admitrace.io/v1alpha1
kind: Scenario
metadata:
  name: yaml-raw-example
compatibilityProfile:
  id: kubernetes-1.36.2-defaults
  kubernetesVersion: 1.36.2
  featureGatePolicy: kubernetes-defaults
configuration:
  validatingWebhookConfiguration:
    apiVersion: admissionregistration.k8s.io/v1
    kind: ValidatingWebhookConfiguration
    webhooks:
      - name: first.policy.example.com
        rules:
          - operations: [CREATE]
            apiGroups: [""]
            apiVersions: [v1]
            resources: [pods]
request:
  kind: {version: v1, kind: Widget}
  resource: {group: example.io, version: v1, resource: widgets}
  operation: CREATE
  scope: Namespaced
  userInfo: {username: alice}
  object:
    apiVersion: example.io/v1
    kind: Widget
    spec:
      extension:
        enabled: true
        weights: [1, 2.5]
      unknown:
        nested: value
  oldObject:
    metadata:
      labels:
        custom.example/key: old
    arbitrary:
      - name: retained
  options:
    apiVersion: meta.k8s.io/v1
    kind: CreateOptions
    custom:
      nested:
        allowed: true
`

	got, err := scenario.Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	assertJSONSemanticallyEqual(t, got.Request.Object, `{"apiVersion":"example.io/v1","kind":"Widget","spec":{"extension":{"enabled":true,"weights":[1,2.5]},"unknown":{"nested":"value"}}}`)
	assertJSONSemanticallyEqual(t, got.Request.OldObject, `{"metadata":{"labels":{"custom.example/key":"old"}},"arbitrary":[{"name":"retained"}]}`)
	assertJSONSemanticallyEqual(t, got.Request.Options, `{"apiVersion":"meta.k8s.io/v1","kind":"CreateOptions","custom":{"nested":{"allowed":true}}}`)
}

func assertJSONSemanticallyEqual(t *testing.T, got []byte, want string) {
	t.Helper()

	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("json.Unmarshal(got) error = %v; got = %s", err, got)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("json.Unmarshal(want) error = %v; want = %s", err, want)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("decoded JSON = %#v, want %#v", gotValue, wantValue)
	}
}

func requireInvalidInput(t *testing.T, err error, wantPath string) {
	t.Helper()

	if err == nil {
		t.Fatal("error = nil, want invalid input")
	}
	if !errors.Is(err, contract.ErrInvalidInput) {
		t.Errorf("errors.Is(error, ErrInvalidInput) = false, error = %v", err)
	}
	var invalid *contract.InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("errors.As(error, *InvalidInputError) = false, error = %v", err)
	}
	if invalid.Field != wantPath {
		t.Errorf("InvalidInputError.Field = %q, want %q; error = %v", invalid.Field, wantPath, err)
	}
}

func newScenario(kind contract.ConfigurationKind) *contract.Scenario {
	input := &contract.Scenario{
		APIVersion:           contract.ScenarioAPIVersion,
		Kind:                 contract.ScenarioKind,
		Metadata:             contract.ScenarioMetadata{Name: "routing-example"},
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		Request: contract.AdmissionRequest{
			Kind:      metav1.GroupVersionKind{Version: "v1", Kind: "Pod"},
			Resource:  metav1.GroupVersionResource{Version: "v1", Resource: "pods"},
			Operation: admissionv1.Create,
			Scope:     contract.RequestScopeNamespaced,
			UserInfo:  authenticationv1.UserInfo{Username: "alice"},
		},
	}
	switch kind {
	case contract.ConfigurationKindValidating:
		input.Configuration.Validating = &kube136.ValidatingWebhookConfiguration{
			TypeMeta: metav1.TypeMeta{
				APIVersion: kube136.AdmissionRegistrationAPIVersion,
				Kind:       string(contract.ConfigurationKindValidating),
			},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{validValidatingWebhook("first.policy.example.com")},
		}
	case contract.ConfigurationKindMutating:
		input.Configuration.Mutating = &kube136.MutatingWebhookConfiguration{
			TypeMeta: metav1.TypeMeta{
				APIVersion: kube136.AdmissionRegistrationAPIVersion,
				Kind:       string(contract.ConfigurationKindMutating),
			},
			Webhooks: []admissionregistrationv1.MutatingWebhook{validMutatingWebhook("first.policy.example.com")},
		}
	}
	return input
}

func validValidatingWebhook(name string) admissionregistrationv1.ValidatingWebhook {
	return admissionregistrationv1.ValidatingWebhook{
		Name: name,
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
				},
			},
		},
	}
}

func validMutatingWebhook(name string) admissionregistrationv1.MutatingWebhook {
	return admissionregistrationv1.MutatingWebhook{
		Name: name,
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
				},
			},
		},
	}
}

func webhookNames(input *contract.Scenario) []string {
	if input.Configuration.Validating != nil {
		names := make([]string, len(input.Configuration.Validating.Webhooks))
		for i := range input.Configuration.Validating.Webhooks {
			names[i] = input.Configuration.Validating.Webhooks[i].Name
		}
		return names
	}
	names := make([]string, len(input.Configuration.Mutating.Webhooks))
	for i := range input.Configuration.Mutating.Webhooks {
		names[i] = input.Configuration.Mutating.Webhooks[i].Name
	}
	return names
}

func scenarioDocument(configuration, requestExtra, rootExtra string) string {
	return `{"apiVersion":"` + contract.ScenarioAPIVersion +
		`","kind":"` + contract.ScenarioKind +
		`","metadata":{"name":"decoder-example"},"compatibilityProfile":{"id":"` + contract.Kubernetes136DefaultProfileID +
		`","kubernetesVersion":"` + kube136.KubernetesVersion +
		`","featureGatePolicy":"` + string(contract.FeatureGatePolicyKubernetesDefaults) +
		`"},"configuration":` + configuration +
		`,"request":{"kind":{"version":"v1","kind":"Pod"},"resource":{"version":"v1","resource":"pods"},"operation":"CREATE","scope":"Namespaced","userInfo":{"username":"alice"}` + requestExtra + `}` + rootExtra + `}`
}

func validatingConfigurationDocument(configurationExtra, webhookExtra string) string {
	return `{"validatingWebhookConfiguration":` + validatingConfigurationValue(configurationExtra, webhookExtra) + `}`
}

func validatingConfigurationValue(configurationExtra, webhookExtra string) string {
	return `{"apiVersion":"` + kube136.AdmissionRegistrationAPIVersion + `","kind":"ValidatingWebhookConfiguration","webhooks":[` + validatingWebhookDocument(webhookExtra) + `]` + configurationExtra + `}`
}

func mutatingConfigurationDocument(configurationExtra, webhookExtra string) string {
	return `{"mutatingWebhookConfiguration":` + mutatingConfigurationValue(configurationExtra, webhookExtra) + `}`
}

func mutatingConfigurationValue(configurationExtra, webhookExtra string) string {
	return `{"apiVersion":"` + kube136.AdmissionRegistrationAPIVersion + `","kind":"MutatingWebhookConfiguration","webhooks":[` + mutatingWebhookDocument(webhookExtra) + `]` + configurationExtra + `}`
}

func validatingWebhookDocument(extra string) string {
	return `{"name":"first.policy.example.com","rules":[{"operations":["CREATE"],"apiGroups":[""],"apiVersions":["v1"],"resources":["pods"]}]` + extra + `}`
}

func mutatingWebhookDocument(extra string) string {
	return `{"name":"first.policy.example.com","rules":[{"operations":["CREATE"],"apiGroups":[""],"apiVersions":["v1"],"resources":["pods"]}]` + extra + `}`
}
