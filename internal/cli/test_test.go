package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/cli"
)

func TestExecuteTestDiscoversScenariosInLexicalOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first := writeTestScenario(t, root, "a.yaml", testScenario("first", "", determinateExpectation("determinate"), ""))
	second := writeTestScenario(t, root, "nested/b.json", testScenario("second", "", determinateExpectation("determinate"), ""))
	writeTestScenario(t, root, "ignored.txt", "not a Scenario")

	code, stdout, stderr := executeTest(t, []string{"test", root, second, "-o", "json"})
	if code != cli.ExitSuccess {
		t.Fatalf("Execute() = %d, want %d; stderr = %q", code, cli.ExitSuccess, stderr)
	}
	if stderr != "" {
		t.Errorf("Execute() stderr = %q, want empty", stderr)
	}

	var report struct {
		Fixtures []struct {
			Path       string `json:"path"`
			ScenarioID string `json:"scenarioId"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	wantPaths := []string{first, second}
	gotPaths := make([]string, len(report.Fixtures))
	gotIDs := make([]string, len(report.Fixtures))
	for i, fixture := range report.Fixtures {
		gotPaths[i] = fixture.Path
		gotIDs[i] = fixture.ScenarioID
	}
	if !slices.Equal(gotPaths, wantPaths) {
		t.Errorf("fixture paths = %v, want %v", gotPaths, wantPaths)
	}
	if want := []string{"first", "second"}; !slices.Equal(gotIDs, want) {
		t.Errorf("fixture scenario IDs = %v, want %v", gotIDs, want)
	}
}

func TestExecuteTestOutputFormatsAreDeterministic(t *testing.T) {
	t.Parallel()

	filename := writeTestScenario(t, t.TempDir(), "scenario.fixture", testScenario(
		"format",
		"",
		determinateExpectation("determinate"),
		"",
	))
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			firstCode, firstOutput, firstStderr := executeTest(t, []string{"test", filename, "-o", format})
			secondCode, secondOutput, secondStderr := executeTest(t, []string{"test", filename, "-o", format})
			if firstCode != cli.ExitSuccess || secondCode != cli.ExitSuccess {
				t.Fatalf("Execute() codes = (%d, %d), want success", firstCode, secondCode)
			}
			if firstOutput != secondOutput {
				t.Errorf("repeated output = %q, want %q", secondOutput, firstOutput)
			}
			if firstStderr+secondStderr != "" {
				t.Errorf("Execute() stderr = %q, want empty", firstStderr+secondStderr)
			}
			for _, want := range []string{"format", "passed"} {
				if !strings.Contains(firstOutput, want) {
					t.Errorf("Execute() output = %q, want substring %q", firstOutput, want)
				}
			}
		})
	}
}

func TestExecuteTestExitCodesAndExpectationDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantCode   cli.ExitCode
		wantOutput []string
	}{
		{
			name:       "matching determinate",
			input:      testScenario("pass", "", determinateExpectation("determinate"), ""),
			wantCode:   cli.ExitSuccess,
			wantOutput: []string{`"status": "passed"`, `"matched": true`},
		},
		{
			name: "mismatched outcome and terminal reason",
			input: testScenario("mismatch", "", `
  - webhookName: policy.example.com
    determination: determinate
    outcome: skipped
    terminalReasonCode: RULE_NO_MATCH`, ""),
			wantCode: cli.ExitExpectationMismatch,
			wantOutput: []string{
				`"status": "mismatched"`,
				`"field": "outcome"`,
				`"field": "terminalReasonCode"`,
			},
		},
		{
			name:       "unexpected incomplete",
			input:      testScenario("incomplete", namespaceSelector, "", ""),
			wantCode:   cli.ExitIncompleteEvaluation,
			wantOutput: []string{`"status": "incomplete"`, `"determination": "indeterminate"`},
		},
		{
			name: "expected incomplete succeeds",
			input: testScenario("expected-incomplete", namespaceSelector, `
  - webhookName: policy.example.com
    determination: indeterminate
    terminalReasonCode: NAMESPACE_CONTEXT_MISSING`, ""),
			wantCode:   cli.ExitSuccess,
			wantOutput: []string{`"status": "passed"`, `"matched": true`, `"determination": "indeterminate"`},
		},
		{
			name: "expected unsupported succeeds",
			input: testScenario("expected-unsupported", `
        sideEffects: Some`, `
  - webhookName: policy.example.com
    determination: unsupported`, `
  dryRun: true`),
			wantCode:   cli.ExitSuccess,
			wantOutput: []string{`"status": "passed"`, `"determination": "unsupported"`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filename := writeTestScenario(t, t.TempDir(), "scenario.yaml", test.input)
			code, stdout, stderr := executeTest(t, []string{"test", filename, "-o", "json"})
			if code != test.wantCode {
				t.Fatalf("Execute() = %d, want %d; stdout = %q; stderr = %q", code, test.wantCode, stdout, stderr)
			}
			if stderr != "" {
				t.Errorf("Execute() stderr = %q, want empty", stderr)
			}
			for _, want := range test.wantOutput {
				if !strings.Contains(stdout, want) {
					t.Errorf("Execute() stdout = %q, want substring %q", stdout, want)
				}
			}
		})
	}
}

func TestExecuteTestAggregatesFailuresUsingExitPriority(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mismatch := writeTestScenario(t, root, "a-mismatch.yaml", testScenario(
		"mismatch",
		"",
		determinateExpectation("indeterminate"),
		"",
	))
	invalid := writeTestScenario(t, root, "z-invalid.yaml", "kind: [")

	code, stdout, stderr := executeTest(t, []string{"test", mismatch, invalid, "-o", "json"})
	if code != cli.ExitInvalidInput {
		t.Fatalf("Execute() = %d, want %d", code, cli.ExitInvalidInput)
	}
	if stderr != "" {
		t.Errorf("Execute() stderr = %q, want empty", stderr)
	}
	for _, want := range []string{`"status": "mismatched"`, `"status": "invalid"`, `"exitCode": 2`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("Execute() stdout = %q, want substring %q", stdout, want)
		}
	}
}

func TestExecuteTestInvalidInputsAndUsage(t *testing.T) {
	t.Parallel()

	t.Run("missing path shows usage", func(t *testing.T) {
		code, stdout, stderr := executeTest(t, []string{"test"})
		if code != cli.ExitInvalidInput || stdout != "" {
			t.Fatalf("Execute() = (%d, %q), want (%d, empty)", code, stdout, cli.ExitInvalidInput)
		}
		for _, want := range []string{"requires at least 1 arg", "Usage:"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("Execute() stderr = %q, want substring %q", stderr, want)
			}
		}
	})

	t.Run("empty directory is structured invalid result", func(t *testing.T) {
		code, stdout, stderr := executeTest(t, []string{"test", t.TempDir(), "-o", "json"})
		if code != cli.ExitInvalidInput || stderr != "" {
			t.Fatalf("Execute() = (%d, stderr %q), want (%d, empty stderr)", code, stderr, cli.ExitInvalidInput)
		}
		for _, want := range []string{`"status": "invalid"`, "contains no .yaml"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Execute() stdout = %q, want substring %q", stdout, want)
			}
		}
	})

	t.Run("help documents discovery contract", func(t *testing.T) {
		code, stdout, stderr := executeTest(t, []string{"test", "--help"})
		if code != cli.ExitSuccess || stderr != "" {
			t.Fatalf("Execute() = (%d, stderr %q), want (%d, empty stderr)", code, stderr, cli.ExitSuccess)
		}
		for _, want := range []string{"regardless of extension", "searched recursively", ".yaml, .yml, and .json", "without following symlink directories", "lexical order"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Execute() stdout = %q, want substring %q", stdout, want)
			}
		}
	})
}

func TestExecuteTestOutputWriteFailureIsInternal(t *testing.T) {
	t.Parallel()

	filename := writeTestScenario(t, t.TempDir(), "scenario.yaml", testScenario("write", "", "", ""))
	var stderr bytes.Buffer
	code := cli.Execute([]string{"test", filename}, strings.NewReader(""), errorWriter{err: fmt.Errorf("closed")}, &stderr, testBuildMetadata())
	if code != cli.ExitInternalError {
		t.Fatalf("Execute() = %d, want %d", code, cli.ExitInternalError)
	}
	if got := stderr.String(); !strings.Contains(got, "write test output: closed") || strings.Contains(got, "Usage:") {
		t.Errorf("Execute() stderr = %q, want internal write error without usage", got)
	}
}

func executeTest(t *testing.T, args []string) (cli.ExitCode, string, string) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Execute(args, strings.NewReader(""), &stdout, &stderr, testBuildMetadata())
	return code, stdout.String(), stderr.String()
}

func writeTestScenario(t *testing.T, root, name, data string) string {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}

func determinateExpectation(determination string) string {
	return fmt.Sprintf(`
  - webhookName: policy.example.com
    determination: %s`, determination)
}

func testScenario(name, webhookExtra, expectations, requestExtra string) string {
	expectationsBlock := ""
	if expectations != "" {
		expectationsBlock = "\nexpectations:" + expectations
	}
	return fmt.Sprintf(`apiVersion: admitrace.io/v1alpha1
kind: Scenario
metadata:
  name: %s
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
  userInfo: {username: test}%s%s
`, name, webhookExtra, requestExtra, expectationsBlock)
}

const namespaceSelector = `
        namespaceSelector:
          matchLabels:
            environment: production`
