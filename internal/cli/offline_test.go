package cli_test

import (
	"bytes"
	"testing"

	"github.com/silbaram/admitrace/internal/cli"
)

func TestRedactionOfflineDeterminismRuntimeSucceedsWithoutNetwork(t *testing.T) {
	proxyURL := "http://127.0.0.1:1"
	t.Setenv("HTTP_PROXY", proxyURL)
	t.Setenv("HTTPS_PROXY", proxyURL)
	t.Setenv("ALL_PROXY", proxyURL)
	t.Setenv("NO_PROXY", "")
	var first []byte
	for i := 0; i < 2; i++ {
		var stdout, stderr bytes.Buffer
		exitCode := cli.Execute(
			[]string{"explain", "-f", "-", "-o", "json"},
			bytes.NewBufferString(testScenario("offline", "", "", "")),
			&stdout,
			&stderr,
			cli.BuildMetadata{},
		)
		if exitCode != cli.ExitSuccess {
			t.Fatalf("Execute() iteration %d exit code = %d, want %d; stderr = %q", i, exitCode, cli.ExitSuccess, stderr.String())
		}
		if i == 0 {
			first = append([]byte(nil), stdout.Bytes()...)
			continue
		}
		if !bytes.Equal(stdout.Bytes(), first) {
			t.Errorf("offline output iteration %d = %q, want %q", i, stdout.Bytes(), first)
		}
	}
}
