package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionProcessUsesBuildMetadata(t *testing.T) {
	command := helperProcess(t, "version", "-o", "json")
	command.Env = append(command.Env, "ADMITRACE_HELPER_BUILD_METADATA=1")

	output, err := command.Output()
	if err != nil {
		t.Fatalf("admitrace version error = %v", err)
	}
	for _, want := range []string{
		`"version": "v0.1.0-process-test"`,
		`"commit": "process-test-commit"`,
		`"buildDate": "2026-07-13T01:02:03Z"`,
	} {
		if got := string(output); !strings.Contains(got, want) {
			t.Errorf("admitrace version output = %q, want substring %q", got, want)
		}
	}
}

func TestInvalidOutputProcessExit(t *testing.T) {
	command := helperProcess(t, "version", "-o", "yaml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("admitrace version error = %v, want process exit error", err)
	}
	if got := exitErr.ExitCode(); got != 2 {
		t.Errorf("admitrace version exit code = %d, want 2", got)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("admitrace version stdout = %q, want empty", got)
	}
	for _, want := range []string{"invalid output format", "Usage:"} {
		if got := stderr.String(); !strings.Contains(got, want) {
			t.Errorf("admitrace version stderr = %q, want substring %q", got, want)
		}
	}
}

func TestExplainProcessFileAndStdinAreEquivalent(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "scenario.yaml")
	if err := os.WriteFile(filename, []byte(processScenario), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	fileCommand := helperProcess(t, "explain", "-f", filename, "-o", "json")
	fileOutput, err := fileCommand.Output()
	if err != nil {
		t.Fatalf("file explain error = %v", err)
	}

	stdinCommand := helperProcess(t, "explain", "-f", "-", "-o", "json")
	stdinCommand.Stdin = strings.NewReader(processScenario)
	stdinOutput, err := stdinCommand.Output()
	if err != nil {
		t.Fatalf("stdin explain error = %v", err)
	}
	if got, want := string(fileOutput), string(stdinOutput); got != want {
		t.Errorf("file output = %q, want stdin output %q", got, want)
	}
	for _, want := range []string{`"determination": "determinate"`, `"outcome": "called"`} {
		if got := string(fileOutput); !strings.Contains(got, want) {
			t.Errorf("explain output = %q, want substring %q", got, want)
		}
	}
}

func TestExplainIncompleteProcessExit(t *testing.T) {
	input := strings.Replace(
		processScenario,
		"        rules:",
		"        namespaceSelector:\n          matchLabels:\n            environment: production\n        rules:",
		1,
	)
	command := helperProcess(t, "explain", "-f", "-", "-o", "text")
	command.Stdin = strings.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("admitrace explain error = %v, want process exit error", err)
	}
	if got := exitErr.ExitCode(); got != 3 {
		t.Errorf("admitrace explain exit code = %d, want 3", got)
	}
	if got := stdout.String(); !strings.Contains(got, "determination: indeterminate") {
		t.Errorf("admitrace explain stdout = %q, want indeterminate result", got)
	}
	if got := stderr.String(); got != "" {
		t.Errorf("admitrace explain stderr = %q, want empty", got)
	}
}

func TestTestCommandProcessExitCodesZeroThroughThree(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantCode int
		want     string
	}{
		{
			name: "matching expectation",
			input: processScenario + `expectations:
  - webhookName: process.policy.example.com
    determination: determinate
`,
			wantCode: 0,
			want:     `"status": "passed"`,
		},
		{
			name: "expectation mismatch",
			input: processScenario + `expectations:
  - webhookName: process.policy.example.com
    determination: indeterminate
`,
			wantCode: 1,
			want:     `"status": "mismatched"`,
		},
		{
			name:     "invalid input",
			input:    "kind: [",
			wantCode: 2,
			want:     `"status": "invalid"`,
		},
		{
			name: "unexpected incomplete",
			input: strings.Replace(
				processScenario,
				"        rules:",
				"        namespaceSelector:\n          matchLabels:\n            environment: production\n        rules:",
				1,
			),
			wantCode: 3,
			want:     `"status": "incomplete"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "scenario.yaml")
			if err := os.WriteFile(filename, []byte(test.input), 0o600); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}

			command := helperProcess(t, "test", filename, "-o", "json")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			err := command.Run()
			if got := processExitCode(err); got != test.wantCode {
				t.Fatalf("admitrace test exit code = %d, want %d; error = %v; stderr = %q", got, test.wantCode, err, stderr.String())
			}
			if got := stderr.String(); got != "" {
				t.Errorf("admitrace test stderr = %q, want empty", got)
			}
			if got := stdout.String(); !strings.Contains(got, test.want) {
				t.Errorf("admitrace test stdout = %q, want substring %q", got, test.want)
			}
		})
	}
}

func TestTestCommandProcessInternalWriteErrorExitFour(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "scenario.yaml")
	if err := os.WriteFile(filename, []byte(processScenario), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	command := helperProcess(t, "test", filename)
	command.Env = append(command.Env, "ADMITRACE_HELPER_CLOSE_STDOUT=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err := command.Run()
	if got := processExitCode(err); got != 4 {
		t.Fatalf("admitrace test exit code = %d, want 4; error = %v; stderr = %q", got, err, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "write test output") {
		t.Errorf("admitrace test stderr = %q, want write error", got)
	}
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func TestAdmitraceHelperProcess(t *testing.T) {
	if os.Getenv("ADMITRACE_HELPER_PROCESS") != "1" {
		return
	}

	separator := 0
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator == 0 {
		os.Exit(4)
	}

	if os.Getenv("ADMITRACE_HELPER_BUILD_METADATA") == "1" {
		version = "v0.1.0-process-test"
		commit = "process-test-commit"
		buildDate = "2026-07-13T01:02:03Z"
	}
	if os.Getenv("ADMITRACE_HELPER_CLOSE_STDOUT") == "1" {
		if err := os.Stdout.Close(); err != nil {
			os.Exit(4)
		}
	}
	os.Args = append([]string{"admitrace"}, os.Args[separator+1:]...)
	main()
}

func helperProcess(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()

	commandArgs := []string{"-test.run=TestAdmitraceHelperProcess", "--"}
	commandArgs = append(commandArgs, args...)
	command := exec.Command(os.Args[0], commandArgs...)
	command.Env = append(os.Environ(), "ADMITRACE_HELPER_PROCESS=1")
	return command
}

const processScenario = `apiVersion: admitrace.io/v1alpha1
kind: Scenario
metadata:
  name: process-explain
compatibilityProfile:
  id: kubernetes-1.36.2-defaults
  kubernetesVersion: 1.36.2
  featureGatePolicy: kubernetes-defaults
configuration:
  validatingWebhookConfiguration:
    apiVersion: admissionregistration.k8s.io/v1
    kind: ValidatingWebhookConfiguration
    webhooks:
      - name: process.policy.example.com
        failurePolicy: Fail
        rules:
          - operations: [CREATE]
            apiGroups: [""]
            apiVersions: [v1]
            resources: [pods]
request:
  kind: {version: v1, kind: Pod}
  resource: {version: v1, resource: pods}
  namespace: default
  operation: CREATE
  scope: Namespaced
  userInfo: {username: process-test}
`
