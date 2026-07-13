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
		return readLimitedScenario(stdin, "-", "read stdin")
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, &contract.InvalidInputError{Field: filename, Err: fmt.Errorf("read Scenario file: %w", err)}
	}
	return readAndCloseScenario(file, filename, "read Scenario file")
}

func readAndCloseScenario(input io.ReadCloser, field, operation string) ([]byte, error) {
	data, readErr := readLimitedScenario(input, field, operation)
	closeErr := input.Close()
	if readErr != nil {
		if closeErr != nil {
			return nil, errors.Join(
				readErr,
				&contract.InvalidInputError{Field: field, Err: fmt.Errorf("close Scenario file: %w", closeErr)},
			)
		}
		return nil, readErr
	}
	if closeErr != nil {
		return nil, &contract.InvalidInputError{Field: field, Err: fmt.Errorf("close Scenario file: %w", closeErr)}
	}
	return data, nil
}

func readLimitedScenario(input io.Reader, field, operation string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(input, scenario.MaximumDocumentBytes+1))
	if err != nil {
		return nil, &contract.InvalidInputError{Field: field, Err: fmt.Errorf("%s: %w", operation, err)}
	}
	if len(data) > scenario.MaximumDocumentBytes {
		return nil, &contract.ResourceLimitError{
			Field:    field,
			Resource: "document bytes",
			Actual:   len(data),
			Limit:    scenario.MaximumDocumentBytes,
		}
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
