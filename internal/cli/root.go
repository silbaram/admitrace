// Package cli contains the AdmiTrace command-line entrypoint.
package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// Execute runs the root command with the provided arguments and streams.
func Execute(args []string, stdout, stderr io.Writer) error {
	command := newRootCommand(stdout, stderr)
	command.SetArgs(args)
	return command.Execute()
}

func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:           "admitrace",
		Short:         "Trace Kubernetes admission decisions",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.SetOut(stdout)
	command.SetErr(stderr)
	return command
}
