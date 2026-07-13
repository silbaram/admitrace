package cli_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/cli"
)

func TestExecuteShowsRootHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if got := cli.Execute(nil, &stdout, &stderr, testBuildMetadata()); got != cli.ExitSuccess {
		t.Fatalf("Execute() = %d, want %d", got, cli.ExitSuccess)
	}

	for _, want := range []string{
		"Trace Kubernetes admission decisions",
		"Usage:",
		"admitrace [command]",
		"explain",
		"test",
		"version",
		"--output string",
	} {
		if got := stdout.String(); !strings.Contains(got, want) {
			t.Errorf("Execute() stdout = %q, want substring %q", got, want)
		}
	}
	if got := stderr.String(); got != "" {
		t.Errorf("Execute() stderr = %q, want empty", got)
	}
	if got := stdout.String(); strings.Contains(got, "completion") {
		t.Errorf("Execute() stdout = %q, want no unapproved completion command", got)
	}
}

func TestExecuteCommandErrorsUseUsagePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "unknown command",
			args: []string{"unknown"},
			want: "unknown command \"unknown\"",
		},
		{
			name: "unknown flag",
			args: []string{"version", "--unknown"},
			want: "unknown flag: --unknown",
		},
		{
			name: "invalid output",
			args: []string{"version", "--output", "yaml"},
			want: "invalid output format \"yaml\": must be text or json",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr bytes.Buffer

			if got := cli.Execute(test.args, &stdout, &stderr, testBuildMetadata()); got != cli.ExitInvalidInput {
				t.Fatalf("Execute() = %d, want %d", got, cli.ExitInvalidInput)
			}
			if got := stdout.String(); got != "" {
				t.Errorf("Execute() stdout = %q, want empty", got)
			}
			for _, want := range []string{"error: " + test.want, "Usage:"} {
				if got := stderr.String(); !strings.Contains(got, want) {
					t.Errorf("Execute() stderr = %q, want substring %q", got, want)
				}
			}
		})
	}
}

func TestExecuteDiagnosticWriteFailureIsInternal(t *testing.T) {
	t.Parallel()

	stderr := failingWriter{err: errors.New("diagnostic stream closed")}
	if got := cli.Execute([]string{"unknown"}, &bytes.Buffer{}, stderr, testBuildMetadata()); got != cli.ExitInternalError {
		t.Errorf("Execute() = %d, want %d", got, cli.ExitInternalError)
	}
}

type failingWriter struct {
	err error
}

func (writer failingWriter) Write(_ []byte) (int, error) {
	return 0, writer.err
}

func testBuildMetadata() cli.BuildMetadata {
	return cli.BuildMetadata{
		Version:   "v0.1.0-test",
		Commit:    "0123456789abcdef",
		BuildDate: "2026-07-13T00:00:00Z",
	}
}
