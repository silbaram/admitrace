package cli_test

import (
	"testing"

	"github.com/silbaram/admitrace/internal/cli"
)

func TestSelectExitCodePriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		codes []cli.ExitCode
		want  cli.ExitCode
	}{
		{name: "empty", want: cli.ExitSuccess},
		{name: "success", codes: []cli.ExitCode{cli.ExitSuccess}, want: cli.ExitSuccess},
		{name: "incomplete over success", codes: []cli.ExitCode{cli.ExitSuccess, cli.ExitIncompleteEvaluation}, want: cli.ExitIncompleteEvaluation},
		{name: "mismatch over incomplete", codes: []cli.ExitCode{cli.ExitIncompleteEvaluation, cli.ExitExpectationMismatch}, want: cli.ExitExpectationMismatch},
		{name: "invalid over mismatch", codes: []cli.ExitCode{cli.ExitExpectationMismatch, cli.ExitInvalidInput}, want: cli.ExitInvalidInput},
		{name: "internal over invalid", codes: []cli.ExitCode{cli.ExitInvalidInput, cli.ExitInternalError}, want: cli.ExitInternalError},
		{name: "order independent", codes: []cli.ExitCode{cli.ExitInternalError, cli.ExitIncompleteEvaluation, cli.ExitInvalidInput}, want: cli.ExitInternalError},
		{name: "unknown is internal", codes: []cli.ExitCode{cli.ExitCode(99)}, want: cli.ExitInternalError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := cli.SelectExitCode(test.codes...); got != test.want {
				t.Errorf("SelectExitCode() = %d, want %d", got, test.want)
			}
		})
	}
}
