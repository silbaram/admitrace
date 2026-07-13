package cli_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/cli"
)

func TestExecuteVersionText(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if got := cli.Execute([]string{"version"}, strings.NewReader(""), &stdout, &stderr, testBuildMetadata()); got != cli.ExitSuccess {
		t.Fatalf("Execute() = %d, want %d", got, cli.ExitSuccess)
	}

	want := `AdmiTrace v0.1.0-test
commit: 0123456789abcdef
build date: 2026-07-13T00:00:00Z
Scenario schema: admitrace.io/v1alpha1, kind Scenario
result schema: admitrace.result/v1alpha1
compatibility profile: kubernetes-1.36.2-defaults
Kubernetes: 1.36.2 (kubernetes-defaults)
Go language: 1.26
Go toolchain: go1.26.5
dependency: github.com/spf13/cobra v1.10.2
dependency: k8s.io/api v0.36.2
dependency: k8s.io/apimachinery v0.36.2
dependency: k8s.io/apiserver v0.36.2
dependency: k8s.io/client-go v0.36.2
dependency: sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730
conformance oracle: Kubernetes 1.36.2, envtest v0.24.1
`
	if got := stdout.String(); got != want {
		t.Errorf("Execute() stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Errorf("Execute() stderr = %q, want empty", got)
	}
}

func TestExecuteVersionJSON(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if got := cli.Execute([]string{"version", "-o", "json"}, strings.NewReader(""), &stdout, &stderr, testBuildMetadata()); got != cli.ExitSuccess {
		t.Fatalf("Execute() = %d, want %d", got, cli.ExitSuccess)
	}

	want := `{
  "cli": {
    "name": "admitrace",
    "version": "v0.1.0-test",
    "commit": "0123456789abcdef",
    "buildDate": "2026-07-13T00:00:00Z"
  },
  "schemas": {
    "scenario": {
      "apiVersion": "admitrace.io/v1alpha1",
      "kind": "Scenario"
    },
    "result": {
      "schemaVersion": "admitrace.result/v1alpha1"
    }
  },
  "compatibilityProfile": {
    "id": "kubernetes-1.36.2-defaults",
    "kubernetesVersion": "1.36.2",
    "featureGatePolicy": "kubernetes-defaults"
  },
  "dependencies": {
    "goLanguage": "1.26",
    "goToolchain": "go1.26.5",
    "modules": [
      {
        "path": "github.com/spf13/cobra",
        "version": "v1.10.2"
      },
      {
        "path": "k8s.io/api",
        "version": "v0.36.2"
      },
      {
        "path": "k8s.io/apimachinery",
        "version": "v0.36.2"
      },
      {
        "path": "k8s.io/apiserver",
        "version": "v0.36.2"
      },
      {
        "path": "k8s.io/client-go",
        "version": "v0.36.2"
      },
      {
        "path": "sigs.k8s.io/json",
        "version": "v0.0.0-20250730193827-2d320260d730"
      }
    ]
  },
  "oracle": {
    "kubernetesVersion": "1.36.2",
    "envtestVersion": "v0.24.1"
  }
}
`
	if got := stdout.String(); got != want {
		t.Errorf("Execute() stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Errorf("Execute() stderr = %q, want empty", got)
	}
}

func TestExecuteVersionAcceptsPersistentOutputBeforeCommand(t *testing.T) {
	t.Parallel()

	var beforeStdout bytes.Buffer
	var beforeStderr bytes.Buffer
	if got := cli.Execute([]string{"--output", "json", "version"}, strings.NewReader(""), &beforeStdout, &beforeStderr, testBuildMetadata()); got != cli.ExitSuccess {
		t.Fatalf("Execute() with output before command = %d, want %d", got, cli.ExitSuccess)
	}

	var afterStdout bytes.Buffer
	var afterStderr bytes.Buffer
	if got := cli.Execute([]string{"version", "--output", "json"}, strings.NewReader(""), &afterStdout, &afterStderr, testBuildMetadata()); got != cli.ExitSuccess {
		t.Fatalf("Execute() with output after command = %d, want %d", got, cli.ExitSuccess)
	}

	if got, want := beforeStdout.String(), afterStdout.String(); got != want {
		t.Errorf("persistent output before command = %q, want %q", got, want)
	}
	if got := beforeStderr.String() + afterStderr.String(); got != "" {
		t.Errorf("Execute() stderr = %q, want empty", got)
	}
}

func TestExecuteVersionWriteFailureIsInternal(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	stdout := errorWriter{err: errors.New("broken pipe")}

	if got := cli.Execute([]string{"version"}, strings.NewReader(""), stdout, &stderr, testBuildMetadata()); got != cli.ExitInternalError {
		t.Fatalf("Execute() = %d, want %d", got, cli.ExitInternalError)
	}
	if got := stderr.String(); !strings.Contains(got, "error: write version output: broken pipe") {
		t.Errorf("Execute() stderr = %q, want write failure", got)
	}
	if got := stderr.String(); strings.Contains(got, "Usage:") {
		t.Errorf("Execute() stderr = %q, want no usage for internal error", got)
	}
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}
