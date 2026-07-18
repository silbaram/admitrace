package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/adapter"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/hydration"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/resourcecatalog"
)

func TestManifestExplainIncompleteContextsExitThreeWithRenderedEnvelope(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	configurationPath := filepath.Join(directory, "webhooks.yaml")
	writeCLIFile(t, configurationPath, validatingCLIConfigurationWithSelector()+"---\n"+validatingCLIIdentityConfiguration())
	resource := `apiVersion: v1
kind: Pod
metadata: {name: example, namespace: team-a}
`
	calls := 0
	code, stdout, stderr := executeManifestExplain(t, []string{
		"explain", "--resource", "-", "--webhook-config", configurationPath, "-o", "text",
	}, resource, &calls)
	if code != ExitIncompleteEvaluation || stderr != "" {
		t.Fatalf("Execute(incomplete) = code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{
		"namespace: missing",
		"identity: missing",
		"NAMESPACE_CONTEXT_MISSING",
		"IDENTITY_CONTEXT_MISSING",
		"called means routing selected the webhook; no HTTP request was sent",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("text output missing %q:\n%s", want, stdout)
		}
	}
}

func TestManifestExplainRendersForbiddenConfigurationAsIncomplete(t *testing.T) {
	t.Parallel()

	reader := &cliFakeReader{
		discovery: hydration.DiscoveryResult{Status: hydration.ReadStatusSuccess, Resources: []resourcecatalog.Resource{
			{Version: "v1", Kind: "Pod", Resource: "pods", Namespaced: true},
		}},
		validating: hydration.ValidatingConfigurationsResult{Status: hydration.ReadStatusForbidden, Err: errors.New("denied")},
		mutating:   hydration.MutatingConfigurationsResult{Status: hydration.ReadStatusForbidden, Err: errors.New("denied")},
	}
	code, stdout, stderr := executeWithPreparedHydration(t, reader, nil)
	if code != ExitIncompleteEvaluation || stderr != "" {
		t.Fatalf("Execute(forbidden configuration) = code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{`"configuration": {`, `"status": "forbidden"`, "provide --webhook-config"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("JSON output missing %q:\n%s", want, stdout)
		}
	}
}

func TestManifestExplainRendersProfileMismatchBeforeReads(t *testing.T) {
	t.Parallel()

	match := manifest.ProfileMatch{
		Status:                    manifest.ProfileMatchMismatch,
		Profile:                   contract.Kubernetes136DefaultProfile(),
		ObservedKubernetesVersion: "1.36.3",
	}
	hydrationErr := &hydration.Error{
		Kind:            hydration.ErrorKindProfileMismatch,
		Operation:       "verify exact Kubernetes profile",
		ObservedVersion: "1.36.3",
		ProfileMatch:    &match,
		Err:             &contract.UnsupportedCapabilityError{Capability: "Kubernetes compatibility profile"},
	}
	code, stdout, stderr := executeWithPreparedHydration(t, nil, hydrationErr)
	if code != ExitIncompleteEvaluation || stderr != "" {
		t.Fatalf("Execute(profile mismatch) = code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{`"status": "mismatch"`, `"observedKubernetesVersion": "1.36.3"`, `"status": "unsupported"`, "PROFILE_MISMATCH"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("JSON output missing %q:\n%s", want, stdout)
		}
	}
}

func TestManifestExplainDocumentIndexedDecodeErrorRemainsInvalid(t *testing.T) {
	t.Parallel()

	calls := 0
	code, stdout, stderr := executeManifestExplain(t, []string{
		"explain", "--resource", "-", "--context", "staging",
	}, "apiVersion: v1\nkind: Pod\nmetadata: {name: first}\n---\nkind: Pod\nmetadata: {name: second}\n", &calls)
	if code != ExitInvalidInput || stdout != "" {
		t.Fatalf("Execute(decode error) = code %d, stdout %q", code, stdout)
	}
	const want = "error: read Scenario file or resource input and decode Scenario: read stdin: stdin document 2: invalid input: field \".apiVersion\": apiVersion is required\n"
	if stderr != want {
		t.Errorf("decode stderr drifted\ngot:  %q\nwant: %q", stderr, want)
	}
	if calls != 0 {
		t.Errorf("hydration preparation after decode failure = %d, want zero", calls)
	}
}

func executeWithPreparedHydration(t *testing.T, reader adapter.Reader, prepareErr error) (ExitCode, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := executeWithDependencies(
		[]string{"explain", "--resource", "-", "--context", "staging", "-o", "json"},
		strings.NewReader("apiVersion: v1\nkind: Pod\nmetadata: {name: example, namespace: team-a}\n"),
		&stdout,
		&stderr,
		BuildMetadata{},
		commandDependencies{prepareHydration: func(context.Context, hydration.Options) (*adapter.Hydration, error) {
			if prepareErr != nil {
				return nil, prepareErr
			}
			return &adapter.Hydration{
				Reader:      reader,
				SourceLabel: "context:staging",
				ProfileMatch: manifest.ProfileMatch{
					Status:                    manifest.ProfileMatchVerified,
					Profile:                   contract.Kubernetes136DefaultProfile(),
					ObservedKubernetesVersion: "1.36.2",
				},
			}, nil
		}},
	)
	return code, stdout.String(), stderr.String()
}

func validatingCLIIdentityConfiguration() string {
	return `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata: {name: identity}
webhooks:
  - name: identity.example.com
    matchConditions:
      - name: identity
        expression: request.userInfo.username == 'alice'
    rules:
      - operations: [CREATE]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
`
}
