package cli

import (
	"errors"
	"fmt"
)

// ExitCode is an AdmiTrace process exit status.
type ExitCode int

const (
	// ExitSuccess reports a determinate explanation or matching expectations.
	ExitSuccess ExitCode = iota
	// ExitExpectationMismatch reports that at least one test expectation did not match.
	ExitExpectationMismatch
	// ExitInvalidInput reports a command usage, schema, or input error.
	ExitInvalidInput
	// ExitIncompleteEvaluation reports an indeterminate or unsupported explanation.
	ExitIncompleteEvaluation
	// ExitInternalError reports an internal AdmiTrace failure.
	ExitInternalError
)

// SelectExitCode returns the highest-priority code from codes. Priority is
// internal error, invalid input, expectation mismatch, incomplete evaluation,
// then success. An unknown code is treated as an internal error.
func SelectExitCode(codes ...ExitCode) ExitCode {
	selected := ExitSuccess
	for _, code := range codes {
		code = normalizeExitCode(code)
		if exitPriority(code) > exitPriority(selected) {
			selected = code
		}
	}
	return selected
}

func normalizeExitCode(code ExitCode) ExitCode {
	switch code {
	case ExitSuccess, ExitExpectationMismatch, ExitInvalidInput, ExitIncompleteEvaluation, ExitInternalError:
		return code
	default:
		return ExitInternalError
	}
}

func exitPriority(code ExitCode) int {
	switch code {
	case ExitSuccess:
		return 0
	case ExitIncompleteEvaluation:
		return 1
	case ExitExpectationMismatch:
		return 2
	case ExitInvalidInput:
		return 3
	case ExitInternalError:
		return 4
	default:
		return 0
	}
}

type commandError struct {
	code      ExitCode
	showUsage bool
	err       error
}

func newCommandError(code ExitCode, showUsage bool, err error) error {
	return &commandError{code: code, showUsage: showUsage, err: err}
}

func (e *commandError) Error() string {
	return e.err.Error()
}

func (e *commandError) Unwrap() error {
	return e.err
}

func classifyCommandError(err error) (ExitCode, bool) {
	var commandErr *commandError
	if errors.As(err, &commandErr) {
		return commandErr.code, commandErr.showUsage
	}
	return ExitInvalidInput, true
}

func internalError(action string, err error) error {
	return newCommandError(ExitInternalError, false, fmt.Errorf("%s: %w", action, err))
}
