// Package cli contains the AdmiTrace command-line entrypoint.
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Execute runs the root command with the provided arguments, streams, and
// build metadata.
func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer, build BuildMetadata) ExitCode {
	exitCode := ExitSuccess
	command := newRootCommand(stdin, stdout, stderr, build, &exitCode)
	command.SetArgs(args)
	executed, err := command.ExecuteC()
	if err == nil {
		return exitCode
	}

	exitCode, showUsage := classifyCommandError(err)
	if writeErr := writeCommandError(executed, stderr, err, showUsage); writeErr != nil {
		return ExitInternalError
	}
	return exitCode
}

func writeCommandError(command *cobra.Command, stderr io.Writer, commandErr error, showUsage bool) error {
	if _, err := fmt.Fprintf(stderr, "error: %v\n", commandErr); err != nil {
		return fmt.Errorf("write command error: %w", err)
	}
	if !showUsage {
		return nil
	}
	if _, err := fmt.Fprintln(stderr); err != nil {
		return fmt.Errorf("write usage separator: %w", err)
	}
	command.SetOut(stderr)
	if err := command.Usage(); err != nil {
		return fmt.Errorf("show usage: %w", err)
	}
	return nil
}

func newRootCommand(stdin io.Reader, stdout, stderr io.Writer, build BuildMetadata, exitCode *ExitCode) *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:           "admitrace",
		Short:         "Trace Kubernetes admission decisions",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if _, err := parseOutputFormat(output); err != nil {
				return newCommandError(ExitInvalidInput, true, err)
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			if err := command.Help(); err != nil {
				return internalError("show root help", err)
			}
			return nil
		},
	}
	command.SetIn(stdin)
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.PersistentFlags().StringVarP(&output, "output", "o", string(outputText), "output format: text or json")
	command.AddCommand(
		newExplainCommand(&output, exitCode),
		newTestCommand(),
		newVersionCommand(&output, build),
	)
	return command
}

func newTestCommand() *cobra.Command {
	return newHelpOnlyCommand("test", "Test Scenario expectations")
}

func newHelpOnlyCommand(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := command.Help(); err != nil {
				return internalError("show command help", err)
			}
			return nil
		},
	}
}
