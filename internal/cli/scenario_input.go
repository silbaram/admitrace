package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/scenario"
)

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
