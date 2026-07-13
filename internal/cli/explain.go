package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	"github.com/silbaram/admitrace/internal/render"
	"github.com/silbaram/admitrace/internal/scenario"
	"github.com/spf13/cobra"
)

func newExplainCommand(output *string, exitCode *ExitCode) *cobra.Command {
	var filename string
	command := &cobra.Command{
		Use:   "explain",
		Short: "Explain admission webhook routing decisions",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if filename == "" {
				return newCommandError(ExitInvalidInput, true, errors.New("required flag \"file\" not set"))
			}
			format, err := parseOutputFormat(*output)
			if err != nil {
				return newCommandError(ExitInvalidInput, true, err)
			}
			result, err := explain(command.Context(), command.InOrStdin(), filename)
			if err != nil {
				if errors.Is(err, contract.ErrInvalidInput) {
					return newCommandError(ExitInvalidInput, false, err)
				}
				return internalError("explain scenario", err)
			}
			content, err := renderExplanation(result, format)
			if err != nil {
				return internalError("render explanation", err)
			}
			if err := writeOutput(command.OutOrStdout(), content); err != nil {
				return internalError("write explanation output", err)
			}
			*exitCode = explanationExitCode(result)
			return nil
		},
	}
	command.Flags().StringVarP(&filename, "file", "f", "", "Scenario file path, or - for stdin")
	return command
}

func explain(ctx context.Context, stdin io.Reader, filename string) (contract.EvaluationResult, error) {
	data, err := readScenario(stdin, filename)
	if err != nil {
		return contract.EvaluationResult{}, err
	}
	input, err := scenario.Decode(data)
	if err != nil {
		return contract.EvaluationResult{}, fmt.Errorf("decode Scenario from %q: %w", filename, err)
	}
	snapshot, err := evaluation.SnapshotFromScenario(*input)
	if err != nil {
		return contract.EvaluationResult{}, fmt.Errorf("prepare Scenario %q: %w", filename, err)
	}
	return evaluation.NewEvaluator().Evaluate(ctx, snapshot), nil
}

func readScenario(stdin io.Reader, filename string) ([]byte, error) {
	if filename == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, &contract.InvalidInputError{Field: "-", Err: fmt.Errorf("read stdin: %w", err)}
		}
		return data, nil
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, &contract.InvalidInputError{Field: filename, Err: fmt.Errorf("read Scenario file: %w", err)}
	}
	return data, nil
}

func renderExplanation(result contract.EvaluationResult, format outputFormat) ([]byte, error) {
	switch format {
	case outputText:
		return render.Text(result)
	case outputJSON:
		return render.JSON(result)
	default:
		return nil, fmt.Errorf("unsupported output format %q", format)
	}
}

func explanationExitCode(result contract.EvaluationResult) ExitCode {
	codes := make([]ExitCode, 0, len(result.Webhooks)+1)
	codes = append(codes, diagnosticExitCode(result.Diagnostics))
	for _, webhook := range result.Webhooks {
		codes = append(codes, diagnosticExitCode(webhook.Diagnostics))
		if webhook.Determination != contract.DeterminationDeterminate {
			codes = append(codes, ExitIncompleteEvaluation)
		}
	}
	return SelectExitCode(codes...)
}

func diagnosticExitCode(diagnostics []contract.Diagnostic) ExitCode {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == contract.ReasonCodeInternalError {
			return ExitInternalError
		}
	}
	return ExitSuccess
}
