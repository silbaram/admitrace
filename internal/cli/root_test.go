package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/cli"
)

func TestExecuteShowsRootHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := cli.Execute(nil, &stdout, &stderr); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	for _, want := range []string{
		"Trace Kubernetes admission decisions",
		"Usage:",
		"admitrace [flags]",
	} {
		if got := stdout.String(); !strings.Contains(got, want) {
			t.Errorf("Execute() stdout = %q, want substring %q", got, want)
		}
	}
	if got := stderr.String(); got != "" {
		t.Errorf("Execute() stderr = %q, want empty", got)
	}
}
