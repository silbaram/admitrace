package contract

import (
	"errors"
	"fmt"
)

var (
	// ErrOutcomeRequiresDeterminate indicates an outcome attached to an incomplete evaluation.
	ErrOutcomeRequiresDeterminate = errors.New("outcome requires a determinate evaluation")
	// ErrDeterminateRequiresOutcome indicates a completed evaluation with no outcome.
	ErrDeterminateRequiresOutcome = errors.New("determinate evaluation requires an outcome")
	// ErrInvalidEnumValue indicates a value outside a declared semantic vocabulary.
	ErrInvalidEnumValue = errors.New("invalid enum value")
	// ErrTerminalTraceRequired indicates a determinate evaluation with no terminal cause.
	ErrTerminalTraceRequired = errors.New("determinate evaluation requires one terminal trace step")
	// ErrMultipleTerminalTraceSteps indicates an ambiguous determinate terminal cause.
	ErrMultipleTerminalTraceSteps = errors.New("determinate evaluation has multiple terminal trace steps")
	// ErrUnregisteredReasonCode indicates a reason code outside the stable registry.
	ErrUnregisteredReasonCode = errors.New("unregistered reason code")
	// ErrDuplicateReasonCode indicates a duplicate entry in the stable registry.
	ErrDuplicateReasonCode = errors.New("duplicate reason code")
	// ErrInvalidTraceState indicates an ambiguous combination of trace result and state flags.
	ErrInvalidTraceState = errors.New("invalid trace state")
)

// ValidationError identifies the contract field and value that violated an invariant.
type ValidationError struct {
	Field string
	Value string
	Err   error
}

// Error returns a deterministic description of the validation failure.
func (e *ValidationError) Error() string {
	if e == nil {
		return "validation error"
	}
	message := e.Field
	if message == "" {
		message = "contract"
	}
	if e.Value != "" {
		message += fmt.Sprintf(" %q", e.Value)
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

// Unwrap exposes the invariant sentinel or wrapped validation cause.
func (e *ValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ValidateReasonCodeRegistry checks registry values and uniqueness in stable order.
func ValidateReasonCodeRegistry() error {
	seen := make(map[ReasonCode]struct{}, len(reasonCodeRegistry))
	for i, definition := range reasonCodeRegistry {
		field := fmt.Sprintf("reasonCodeRegistry[%d]", i)
		if definition.Code == "" {
			return &ValidationError{Field: field + ".code", Err: ErrUnregisteredReasonCode}
		}
		if _, duplicate := seen[definition.Code]; duplicate {
			return &ValidationError{Field: field + ".code", Value: string(definition.Code), Err: ErrDuplicateReasonCode}
		}
		seen[definition.Code] = struct{}{}
		if !definition.Disposition.IsValid() {
			return &ValidationError{Field: field + ".disposition", Value: string(definition.Disposition), Err: ErrInvalidEnumValue}
		}
	}
	return nil
}
