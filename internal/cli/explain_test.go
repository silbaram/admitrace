package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/cli"
	"github.com/silbaram/admitrace/internal/contract"
)

func TestExecuteExplainFileAndStdinAreEquivalent(t *testing.T) {
	t.Parallel()

	input := explainScenario("", "")
	filename := filepath.Join(t.TempDir(), "scenario.yaml")
	if err := os.WriteFile(filename, []byte(input), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			fileCode, fileStdout, fileStderr := executeExplain(
				t,
				[]string{"explain", "-f", filename, "-o", format},
				"ignored",
			)
			stdinCode, stdinStdout, stdinStderr := executeExplain(
				t,
				[]string{"explain", "-f", "-", "-o", format},
				input,
			)

			if fileCode != cli.ExitSuccess || stdinCode != cli.ExitSuccess {
				t.Fatalf("Execute() codes = (%d, %d), want (%d, %d)", fileCode, stdinCode, cli.ExitSuccess, cli.ExitSuccess)
			}
			if fileStdout != stdinStdout {
				t.Errorf("file output = %q, want stdin output %q", fileStdout, stdinStdout)
			}
			if got := fileStderr + stdinStderr; got != "" {
				t.Errorf("Execute() stderr = %q, want empty", got)
			}
			assertCalledExplanation(t, format, fileStdout)
		})
	}
}

func TestExecuteExplainDeterminateOutcomesExitSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		webhookYAML string
		wantOutcome contract.Outcome
	}{
		{name: "called", wantOutcome: contract.OutcomeCalled},
		{
			name: "rejected before call",
			webhookYAML: `
        matchConditions:
          - name: runtime-error
            expression: "1 / 0 == 0"`,
			wantOutcome: contract.OutcomeRejectedBeforeCall,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			code, stdout, stderr := executeExplain(
				t,
				[]string{"explain", "-f", "-", "-o", "json"},
				explainScenario(test.webhookYAML, ""),
			)
			if code != cli.ExitSuccess {
				t.Fatalf("Execute() = %d, want %d; stderr = %q", code, cli.ExitSuccess, stderr)
			}
			if stderr != "" {
				t.Errorf("Execute() stderr = %q, want empty", stderr)
			}

			var result contract.EvaluationResult
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if got := result.Webhooks[0].Outcome; got == nil || *got != test.wantOutcome {
				t.Errorf("outcome = %v, want %q", got, test.wantOutcome)
			}
		})
	}
}

func TestExecuteExplainIncompleteResultsExitThree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		webhookYAML       string
		requestYAML       string
		wantDetermination contract.Determination
	}{
		{
			name: "indeterminate",
			webhookYAML: `
        namespaceSelector:
          matchLabels:
            environment: production`,
			wantDetermination: contract.DeterminationIndeterminate,
		},
		{
			name: "unsupported",
			webhookYAML: `
        sideEffects: Some`,
			requestYAML: `
  dryRun: true`,
			wantDetermination: contract.DeterminationUnsupported,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			code, stdout, stderr := executeExplain(
				t,
				[]string{"explain", "-f", "-", "-o", "json"},
				explainScenario(test.webhookYAML, test.requestYAML),
			)
			if code != cli.ExitIncompleteEvaluation {
				t.Fatalf("Execute() = %d, want %d; stderr = %q", code, cli.ExitIncompleteEvaluation, stderr)
			}
			if stdout == "" {
				t.Fatal("Execute() stdout is empty, want rendered result")
			}
			if stderr != "" {
				t.Errorf("Execute() stderr = %q, want empty", stderr)
			}

			var result contract.EvaluationResult
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if got := result.Webhooks[0].Determination; got != test.wantDetermination {
				t.Errorf("determination = %q, want %q", got, test.wantDetermination)
			}
		})
	}
}

func TestExecuteExplainInputErrorsExitTwo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		stdin     string
		wantError string
		wantUsage bool
	}{
		{
			name:      "missing file flag",
			args:      []string{"explain"},
			wantError: `required flag "file" not set`,
			wantUsage: true,
		},
		{
			name:      "unreadable file",
			args:      []string{"explain", "-f", filepath.Join(t.TempDir(), "missing.yaml")},
			wantError: "read Scenario file",
		},
		{
			name:      "invalid document",
			args:      []string{"explain", "-f", "-"},
			stdin:     "kind: [",
			wantError: "decode Scenario",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			code, stdout, stderr := executeExplain(t, test.args, test.stdin)
			if code != cli.ExitInvalidInput {
				t.Fatalf("Execute() = %d, want %d", code, cli.ExitInvalidInput)
			}
			if stdout != "" {
				t.Errorf("Execute() stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, test.wantError) {
				t.Errorf("Execute() stderr = %q, want substring %q", stderr, test.wantError)
			}
			if got := strings.Contains(stderr, "Usage:"); got != test.wantUsage {
				t.Errorf("Execute() usage present = %t, want %t; stderr = %q", got, test.wantUsage, stderr)
			}
		})
	}
}

func TestExecuteExplainStreamFailures(t *testing.T) {
	t.Parallel()

	t.Run("stdin read is invalid input", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		stdin := errorReader{err: errors.New("input closed")}

		if got := cli.Execute([]string{"explain", "-f", "-"}, stdin, &stdout, &stderr, testBuildMetadata()); got != cli.ExitInvalidInput {
			t.Fatalf("Execute() = %d, want %d", got, cli.ExitInvalidInput)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "read stdin") {
			t.Errorf("streams = (%q, %q), want empty stdout and read error on stderr", stdout.String(), stderr.String())
		}
	})

	t.Run("stdout write is internal error", func(t *testing.T) {
		var stderr bytes.Buffer
		stdout := errorWriter{err: errors.New("output closed")}

		if got := cli.Execute(
			[]string{"explain", "-f", "-"},
			strings.NewReader(explainScenario("", "")),
			stdout,
			&stderr,
			testBuildMetadata(),
		); got != cli.ExitInternalError {
			t.Fatalf("Execute() = %d, want %d", got, cli.ExitInternalError)
		}
		if got := stderr.String(); !strings.Contains(got, "write explanation output: output closed") || strings.Contains(got, "Usage:") {
			t.Errorf("Execute() stderr = %q, want internal write error without usage", got)
		}
	})
}

type errorReader struct {
	err error
}

func (reader errorReader) Read(_ []byte) (int, error) {
	return 0, reader.err
}

func executeExplain(t *testing.T, args []string, stdin string) (cli.ExitCode, string, string) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Execute(args, strings.NewReader(stdin), &stdout, &stderr, testBuildMetadata())
	return code, stdout.String(), stderr.String()
}

func assertCalledExplanation(t *testing.T, format, output string) {
	t.Helper()

	if format == "text" {
		for _, want := range []string{"determination: determinate", "outcome: called"} {
			if !strings.Contains(output, want) {
				t.Errorf("text output = %q, want substring %q", output, want)
			}
		}
		return
	}
	var result contract.EvaluationResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := result.Webhooks[0].Determination; got != contract.DeterminationDeterminate {
		t.Errorf("determination = %q, want %q", got, contract.DeterminationDeterminate)
	}
	if got := result.Webhooks[0].Outcome; got == nil || *got != contract.OutcomeCalled {
		t.Errorf("outcome = %v, want %q", got, contract.OutcomeCalled)
	}
}

func explainScenario(webhookYAML, requestYAML string) string {
	return fmt.Sprintf(`apiVersion: admitrace.io/v1alpha1
kind: Scenario
metadata:
  name: cli-explain
compatibilityProfile:
  id: kubernetes-1.36.2-defaults
  kubernetesVersion: 1.36.2
  featureGatePolicy: kubernetes-defaults
configuration:
  validatingWebhookConfiguration:
    apiVersion: admissionregistration.k8s.io/v1
    kind: ValidatingWebhookConfiguration
    webhooks:
      - name: policy.example.com
        failurePolicy: Fail
        rules:
          - operations: [CREATE]
            apiGroups: [""]
            apiVersions: [v1]
            resources: [pods]%s
request:
  kind: {version: v1, kind: Pod}
  resource: {version: v1, resource: pods}
  namespace: default
  operation: CREATE
  scope: Namespaced
  userInfo: {username: alice}%s
`, webhookYAML, requestYAML)
}
