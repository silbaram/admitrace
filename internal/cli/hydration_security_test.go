package cli

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestHydrationNamespaceFailuresStayIncompleteWithFileFirstConfiguration(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	configurationPath := filepath.Join(directory, "configuration.yaml")
	writeCLIFile(t, configurationPath, validatingCLIConfigurationWithSelector())

	tests := []struct {
		name       string
		readStatus hydration.ReadStatus
		wantStatus manifest.CompletenessStatus
	}{
		{name: "forbidden", readStatus: hydration.ReadStatusForbidden, wantStatus: manifest.CompletenessForbidden},
		{name: "not found", readStatus: hydration.ReadStatusNotFound, wantStatus: manifest.CompletenessMissing},
		{name: "unavailable", readStatus: hydration.ReadStatusUnavailable, wantStatus: manifest.CompletenessMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reader := &cliFakeReader{
				discovery: hydration.DiscoveryResult{Status: hydration.ReadStatusSuccess, Resources: []resourcecatalog.Resource{
					{Version: "v1", Kind: "Pod", Resource: "pods", Namespaced: true},
				}},
				validating: hydration.ValidatingConfigurationsResult{Status: hydration.ReadStatusUnavailable, Err: errors.New("file precedence must skip this LIST")},
				mutating:   hydration.MutatingConfigurationsResult{Status: hydration.ReadStatusUnavailable, Err: errors.New("file precedence must skip this LIST")},
				namespace:  hydration.NamespaceResult{Status: test.readStatus, Err: errors.New("simulated Namespace read failure")},
			}
			code, stdout, stderr := executeHydrationSecurityExplain(t, reader, []string{
				"explain", "--resource", "-", "--context", "security", "--webhook-config", configurationPath, "-o", "json",
			})
			if code != ExitIncompleteEvaluation || stderr != "" {
				t.Fatalf("Execute(%s) = code %d, stderr %q; want incomplete with no stderr", test.name, code, stderr)
			}
			var explanation manifest.ManifestExplanation
			if err := json.Unmarshal([]byte(stdout), &explanation); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; output = %s", err, stdout)
			}
			if explanation.ContextCompleteness.Configuration.Status != manifest.CompletenessProvided {
				t.Errorf("configuration completeness = %q, want provided", explanation.ContextCompleteness.Configuration.Status)
			}
			if explanation.ContextCompleteness.Namespace.Status != test.wantStatus {
				t.Errorf("Namespace completeness = %q, want %q", explanation.ContextCompleteness.Namespace.Status, test.wantStatus)
			}
			if reader.validatingCalls != 0 || reader.mutatingCalls != 0 || reader.namespaceCalls != 1 {
				t.Errorf("cluster reads = validating %d, mutating %d, Namespace %d; want 0/0/1", reader.validatingCalls, reader.mutatingCalls, reader.namespaceCalls)
			}
			if !strings.Contains(stdout, "Namespace selector context is unavailable; provide --namespace-object") || terminalReason(explanation.Evaluations[0].Result) != contract.ReasonCodeNamespaceContextMissing {
				t.Errorf("fail-closed Namespace guidance/result missing: %s", stdout)
			}
		})
	}
}

func TestHydrationConfigurationForbiddenHasStableFileFallbackGuidance(t *testing.T) {
	t.Parallel()

	reader := &cliFakeReader{
		discovery: hydration.DiscoveryResult{Status: hydration.ReadStatusSuccess, Resources: []resourcecatalog.Resource{
			{Version: "v1", Kind: "Pod", Resource: "pods", Namespaced: true},
		}},
		validating: hydration.ValidatingConfigurationsResult{Status: hydration.ReadStatusForbidden, Err: errors.New("denied")},
		mutating:   hydration.MutatingConfigurationsResult{Status: hydration.ReadStatusForbidden, Err: errors.New("denied")},
	}
	code, stdout, stderr := executeHydrationSecurityExplain(t, reader, []string{
		"explain", "--resource", "-", "--context", "security", "-o", "json",
	})
	if code != ExitIncompleteEvaluation || stderr != "" {
		t.Fatalf("Execute(configuration forbidden) = code %d, stderr %q", code, stderr)
	}
	var explanation manifest.ManifestExplanation
	if err := json.Unmarshal([]byte(stdout), &explanation); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output = %s", err, stdout)
	}
	if explanation.ContextCompleteness.Configuration.Status != manifest.CompletenessForbidden || len(explanation.Evaluations) != 0 {
		t.Errorf("forbidden configuration explanation = %#v", explanation)
	}
	if !strings.Contains(stdout, "cluster webhook configurations are unavailable; provide --webhook-config") {
		t.Errorf("configuration fallback guidance missing: %s", stdout)
	}
	if reader.validatingCalls != 1 || reader.mutatingCalls != 1 || reader.namespaceCalls != 0 {
		t.Errorf("cluster reads = validating %d, mutating %d, Namespace %d; want 1/1/0", reader.validatingCalls, reader.mutatingCalls, reader.namespaceCalls)
	}
}

func TestHydrationProfileMismatchRendersStableDiagnosticEnvelope(t *testing.T) {
	t.Parallel()

	for _, observed := range []string{"1.36.1", "1.36.3", "1.36.2-gke.1", "1.37.0"} {
		t.Run(observed, func(t *testing.T) {
			t.Parallel()
			prepareCalls := 0
			match := manifest.ProfileMatch{
				Status:                    manifest.ProfileMatchMismatch,
				Profile:                   contract.Kubernetes136DefaultProfile(),
				ObservedKubernetesVersion: observed,
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := executeWithDependencies(
				[]string{"explain", "--resource", "-", "--context", "security", "-o", "json"},
				strings.NewReader("apiVersion: v1\nkind: Pod\nmetadata: {name: example}\n"),
				&stdout,
				&stderr,
				BuildMetadata{},
				commandDependencies{prepareHydration: func(context.Context, hydration.Options) (*adapter.Hydration, error) {
					prepareCalls++
					return nil, &hydration.Error{
						Kind:            hydration.ErrorKindProfileMismatch,
						Operation:       "verify exact Kubernetes profile",
						ObservedVersion: observed,
						ProfileMatch:    &match,
						Err:             &contract.UnsupportedCapabilityError{Capability: "Kubernetes compatibility profile"},
					}
				}},
			)
			if code != ExitIncompleteEvaluation || stderr.String() != "" || prepareCalls != 1 {
				t.Fatalf("Execute(%s) = code %d, stderr %q, prepare calls %d", observed, code, stderr.String(), prepareCalls)
			}
			for _, want := range []string{`"status": "mismatch"`, `"observedKubernetesVersion": "` + observed + `"`, "PROFILE_MISMATCH"} {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("profile mismatch output missing %q: %s", want, stdout.String())
				}
			}
		})
	}
}

func executeHydrationSecurityExplain(t *testing.T, reader adapter.Reader, args []string) (ExitCode, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := executeWithDependencies(
		args,
		strings.NewReader("apiVersion: v1\nkind: Pod\nmetadata: {name: example, namespace: team-a}\n"),
		&stdout,
		&stderr,
		BuildMetadata{},
		commandDependencies{prepareHydration: func(context.Context, hydration.Options) (*adapter.Hydration, error) {
			return &adapter.Hydration{
				Reader:      reader,
				SourceLabel: "context:security",
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
