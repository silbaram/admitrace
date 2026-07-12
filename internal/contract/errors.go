package contract

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidInput is the category sentinel for invalid contract input.
	ErrInvalidInput = errors.New("invalid input")
	// ErrMissingContext is the category sentinel for unavailable fixture context.
	ErrMissingContext = errors.New("missing context")
	// ErrUnsupportedCapability is the category sentinel for unsupported evaluation semantics.
	ErrUnsupportedCapability = errors.New("unsupported capability")
	// ErrKubernetesEvaluation is the category sentinel for Kubernetes semantic evaluation failures.
	ErrKubernetesEvaluation = errors.New("kubernetes evaluation error")
	// ErrInternal is the category sentinel for internal failures.
	ErrInternal = errors.New("internal error")
)

// InvalidInputError describes input that does not satisfy the evaluation contract.
type InvalidInputError struct {
	Field string
	Err   error
}

// Error returns the category, field, and wrapped cause description.
func (e *InvalidInputError) Error() string {
	if e == nil {
		return ErrInvalidInput.Error()
	}
	return formatCategoryError(ErrInvalidInput, fieldDetail(e.Field), e.Err)
}

// Unwrap exposes the underlying cause.
func (e *InvalidInputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is classifies InvalidInputError independently from its message.
func (e *InvalidInputError) Is(target error) bool {
	return target == ErrInvalidInput
}

// MissingContextError describes fixture context required for a determinate evaluation.
type MissingContextError struct {
	Context   string
	Reference string
	Err       error
}

// Error returns the category, context, and wrapped cause description.
func (e *MissingContextError) Error() string {
	if e == nil {
		return ErrMissingContext.Error()
	}
	detail := e.Context
	if detail == "" {
		detail = "context"
	}
	if e.Reference != "" {
		detail = fmt.Sprintf("%s %q", detail, e.Reference)
	}
	return formatCategoryError(ErrMissingContext, detail, e.Err)
}

// Unwrap exposes the underlying cause.
func (e *MissingContextError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is classifies MissingContextError independently from its message.
func (e *MissingContextError) Is(target error) bool {
	return target == ErrMissingContext
}

// UnsupportedCapabilityError describes semantics outside the selected profile.
type UnsupportedCapabilityError struct {
	Capability string
	Err        error
}

// Error returns the category, capability, and wrapped cause description.
func (e *UnsupportedCapabilityError) Error() string {
	if e == nil {
		return ErrUnsupportedCapability.Error()
	}
	return formatCategoryError(ErrUnsupportedCapability, e.Capability, e.Err)
}

// Unwrap exposes the underlying cause.
func (e *UnsupportedCapabilityError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is classifies UnsupportedCapabilityError independently from its message.
func (e *UnsupportedCapabilityError) Is(target error) bool {
	return target == ErrUnsupportedCapability
}

// KubernetesEvaluationError describes a failure returned by Kubernetes evaluation semantics.
type KubernetesEvaluationError struct {
	Operation string
	Err       error
}

// Error returns the category, operation, and wrapped cause description.
func (e *KubernetesEvaluationError) Error() string {
	if e == nil {
		return ErrKubernetesEvaluation.Error()
	}
	return formatCategoryError(ErrKubernetesEvaluation, e.Operation, e.Err)
}

// Unwrap exposes the underlying cause.
func (e *KubernetesEvaluationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is classifies KubernetesEvaluationError independently from its message.
func (e *KubernetesEvaluationError) Is(target error) bool {
	return target == ErrKubernetesEvaluation
}

// InternalError describes an internal invariant or operation failure.
type InternalError struct {
	Operation string
	Err       error
}

// Error returns the category, operation, and wrapped cause description.
func (e *InternalError) Error() string {
	if e == nil {
		return ErrInternal.Error()
	}
	return formatCategoryError(ErrInternal, e.Operation, e.Err)
}

// Unwrap exposes the underlying cause.
func (e *InternalError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is classifies InternalError independently from its message.
func (e *InternalError) Is(target error) bool {
	return target == ErrInternal
}

func fieldDetail(field string) string {
	if field == "" {
		return ""
	}
	return fmt.Sprintf("field %q", field)
}

func formatCategoryError(category error, detail string, cause error) string {
	message := category.Error()
	if detail != "" {
		message += ": " + detail
	}
	if cause != nil {
		message += ": " + cause.Error()
	}
	return message
}
