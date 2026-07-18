package manifest_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/manifest"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
)

func TestNewIdentityUsesOnlyExplicitFlags(t *testing.T) {
	t.Parallel()

	identity, err := manifest.NewIdentity(manifest.IdentityOptions{
		User:      "alice",
		Groups:    []string{"developers", "developers"},
		UID:       "uid-1",
		UserExtra: []string{`scopes=read,write`, `scopes=read`, `tenant\=id=blue\,green,,blue\\red`, `empty=`},
	})
	if err != nil {
		t.Fatalf("NewIdentity() error = %v", err)
	}
	want := authenticationv1.UserInfo{
		Username: "alice",
		UID:      "uid-1",
		Groups:   []string{"developers", "developers"},
		Extra: map[string]authenticationv1.ExtraValue{
			"scopes":    {"read", "write", "read"},
			"tenant=id": {"blue,green", "", `blue\red`},
			"empty":     {""},
		},
	}
	if !identity.Provided() || !reflect.DeepEqual(identity.UserInfo(), want) {
		t.Errorf("identity = (%t, %#v), want explicitly provided %#v", identity.Provided(), identity.UserInfo(), want)
	}

	copy := identity.UserInfo()
	copy.Groups[0] = "changed"
	copy.Extra["scopes"][0] = "changed"
	if !reflect.DeepEqual(identity.UserInfo(), want) {
		t.Error("Identity retained mutation through returned UserInfo")
	}
}

func TestNewIdentityNeverInfersKubeOrWorkloadIdentity(t *testing.T) {
	t.Parallel()

	identity, err := manifest.NewIdentity(manifest.IdentityOptions{})
	if err != nil {
		t.Fatalf("NewIdentity() error = %v", err)
	}
	if identity.Provided() || !reflect.DeepEqual(identity.UserInfo(), authenticationv1.UserInfo{}) {
		t.Errorf("empty explicit flags produced identity %#v", identity.UserInfo())
	}

	resource := decodeResourceDocuments(t, `apiVersion: v1
kind: Pod
metadata:
  name: workload
  annotations:
    kubeconfig-user: cluster-admin
spec:
  serviceAccountName: workload-service-account
`)
	built, err := manifest.BuildScenarios(resource, decodeConfigurationDocuments(t, validatingConfiguration), manifest.OfflineResolver{}, manifest.BuildOptions{Identity: identity})
	if err != nil {
		t.Fatalf("BuildScenarios() error = %v", err)
	}
	if built[0].IdentityProvided || built[0].Scenario.Request.UserInfo.Username != "" {
		t.Errorf("builder inferred admission identity %#v from workload data", built[0].Scenario.Request.UserInfo)
	}
}

func TestNewIdentityRequiresUserForSupplementalFlags(t *testing.T) {
	t.Parallel()

	tests := []manifest.IdentityOptions{
		{Groups: []string{"developers"}},
		{UID: "uid-1"},
		{UserExtra: []string{"tenant=blue"}},
	}
	for _, options := range tests {
		if _, err := manifest.NewIdentity(options); !errors.Is(err, contract.ErrInvalidInput) {
			t.Errorf("NewIdentity(%#v) error = %v, want invalid input", options, err)
		}
	}
}

func TestParseUserExtraRejectsNonCanonicalEncoding(t *testing.T) {
	t.Parallel()

	tests := []string{"missing-equals", "=value", `key=value=other`, `key=value\q`, `key=value\`}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := manifest.ParseUserExtra([]string{input})
			if !errors.Is(err, contract.ErrInvalidInput) {
				t.Fatalf("ParseUserExtra(%q) error = %v, want invalid input", input, err)
			}
		})
	}
}

func TestUserExtraHelpContract(t *testing.T) {
	t.Parallel()

	const want = `admission user extra as key=value[,value...]; repeat to append values; escape '\', '=', and ',' with a backslash; empty and duplicate values are preserved`
	if manifest.UserExtraHelp != want {
		t.Errorf("UserExtraHelp = %q, want %q", manifest.UserExtraHelp, want)
	}
}

func TestEvaluateBuiltScenarioFailsClosedForMissingIdentity(t *testing.T) {
	t.Parallel()

	built := buildIdentityScenario(t, identityMatchConfiguration, manifest.Identity{})
	evaluated, err := manifest.EvaluateBuiltScenario(context.Background(), built)
	if err != nil {
		t.Fatalf("EvaluateBuiltScenario() error = %v", err)
	}
	webhook := evaluated.Result.Webhooks[0]
	if webhook.Determination != contract.DeterminationIndeterminate || webhook.Outcome != nil {
		t.Errorf("webhook result = (%q, %v), want indeterminate/nil", webhook.Determination, webhook.Outcome)
	}
	if terminalReasonCode(webhook) != contract.ReasonCodeIdentityContextMissing {
		t.Errorf("terminal reason = %q, want identity context missing", terminalReasonCode(webhook))
	}
	if len(evaluated.Diagnostics) != 1 || evaluated.Diagnostics[0].Code != manifest.DiagnosticCodeIdentityContextMissing {
		t.Errorf("adapter diagnostics = %#v, want identity context missing", evaluated.Diagnostics)
	}

	identity := newTestIdentity(t, manifest.IdentityOptions{User: "alice"})
	built = buildIdentityScenario(t, identityMatchConfiguration, identity)
	evaluated, err = manifest.EvaluateBuiltScenario(context.Background(), built)
	if err != nil {
		t.Fatalf("EvaluateBuiltScenario(explicit) error = %v", err)
	}
	webhook = evaluated.Result.Webhooks[0]
	if webhook.Determination != contract.DeterminationDeterminate || webhook.Outcome == nil || *webhook.Outcome != contract.OutcomeCalled {
		t.Errorf("explicit identity result = (%q, %v), want determinate/called", webhook.Determination, webhook.Outcome)
	}
	if len(evaluated.Diagnostics) != 0 {
		t.Errorf("explicit identity adapter diagnostics = %#v, want none", evaluated.Diagnostics)
	}
}

func TestEvaluateBuiltScenarioReportsMissingAuthorizerContext(t *testing.T) {
	t.Parallel()

	built := buildIdentityScenario(t, authorizerMatchConfiguration, manifest.Identity{})
	evaluated, err := manifest.EvaluateBuiltScenario(context.Background(), built)
	if err != nil {
		t.Fatalf("EvaluateBuiltScenario() error = %v", err)
	}
	webhook := evaluated.Result.Webhooks[0]
	if webhook.Determination != contract.DeterminationIndeterminate || terminalReasonCode(webhook) != contract.ReasonCodeAuthorizationContextMissing {
		t.Errorf("authorizer result = (%q, %q), want indeterminate/authorization missing", webhook.Determination, terminalReasonCode(webhook))
	}
	if len(evaluated.Diagnostics) != 1 || evaluated.Diagnostics[0].Code != manifest.DiagnosticCodeIncomplete {
		t.Errorf("adapter diagnostics = %#v, want incomplete", evaluated.Diagnostics)
	}
}

func TestValidateOperationRejectsOldObjectOperations(t *testing.T) {
	t.Parallel()

	resources := decodeResourceDocuments(t, `apiVersion: v1
kind: Pod
metadata: {name: example}
`)
	configurations := decodeConfigurationDocuments(t, validatingConfiguration)
	for _, operation := range []admissionv1.Operation{admissionv1.Update, admissionv1.Delete, admissionv1.Connect} {
		t.Run(string(operation), func(t *testing.T) {
			t.Parallel()
			built, err := manifest.BuildScenarios(resources, configurations, manifest.OfflineResolver{}, manifest.BuildOptions{Operation: operation})
			if built != nil {
				t.Errorf("BuildScenarios(%s) returned %d Scenarios, want no oldObject-capable output", operation, len(built))
			}
			if !errors.Is(err, contract.ErrUnsupportedCapability) || !strings.Contains(err.Error(), "oldObject is never hydrated") {
				t.Fatalf("BuildScenarios(%s) error = %v, want clear unsupported oldObject guard", operation, err)
			}
			var operationError *manifest.OperationError
			if !errors.As(err, &operationError) || operationError.Diagnostic.Code != manifest.DiagnosticCodeUnsupportedOperation {
				t.Fatalf("OperationError = %#v, want unsupported operation diagnostic", operationError)
			}
			if err := operationError.Diagnostic.Validate(); err != nil {
				t.Errorf("operation diagnostic validation error = %v", err)
			}
		})
	}
	if err := manifest.ValidateOperation(""); err != nil {
		t.Errorf("ValidateOperation(default) error = %v", err)
	}
	if err := manifest.ValidateOperation(admissionv1.Create); err != nil {
		t.Errorf("ValidateOperation(CREATE) error = %v", err)
	}
}

func buildIdentityScenario(t *testing.T, configuration string, identity manifest.Identity) manifest.BuiltScenario {
	t.Helper()
	resources := decodeResourceDocuments(t, `apiVersion: v1
kind: Pod
metadata:
  name: example
  namespace: default
`)
	built, err := manifest.BuildScenarios(resources, decodeConfigurationDocuments(t, configuration), manifest.OfflineResolver{}, manifest.BuildOptions{Identity: identity})
	if err != nil {
		t.Fatalf("BuildScenarios() error = %v", err)
	}
	return built[0]
}

func terminalReasonCode(webhook contract.WebhookEvaluation) contract.ReasonCode {
	for index := len(webhook.Trace) - 1; index >= 0; index-- {
		if webhook.Trace[index].Terminal {
			return webhook.Trace[index].ReasonCode
		}
	}
	return ""
}

const identityMatchConfiguration = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: identity
webhooks:
  - name: identity.example.com
    rules:
      - operations: [CREATE]
        apiGroups: [""]
        apiVersions: [v1]
        resources: [pods]
    matchConditions:
      - name: explicit-user
        expression: request.userInfo.username == 'alice'
`

const authorizerMatchConfiguration = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: authorizer
webhooks:
  - name: authorizer.example.com
    rules:
      - operations: [CREATE]
        apiGroups: [""]
        apiVersions: [v1]
        resources: [pods]
    matchConditions:
      - name: authorization
        expression: authorizer.group('apps').resource('deployments').namespace('default').name('demo').check('get').allowed()
`
