package contract_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
)

func TestEvaluationErrorCategories(t *testing.T) {
	t.Parallel()

	cause := errors.New("fixture failed")
	tests := []struct {
		name     string
		typed    error
		category error
		matches  func(error) bool
	}{
		{
			name:     "invalid input",
			typed:    &contract.InvalidInputError{Field: "request.kind", Err: cause},
			category: contract.ErrInvalidInput,
			matches: func(err error) bool {
				var target *contract.InvalidInputError
				return errors.As(err, &target) && target.Field == "request.kind"
			},
		},
		{
			name:     "missing context",
			typed:    &contract.MissingContextError{Context: "namespace", Reference: "test", Err: cause},
			category: contract.ErrMissingContext,
			matches: func(err error) bool {
				var target *contract.MissingContextError
				return errors.As(err, &target) && target.Context == "namespace"
			},
		},
		{
			name:     "unsupported capability",
			typed:    &contract.UnsupportedCapabilityError{Capability: "custom-feature-gate", Err: cause},
			category: contract.ErrUnsupportedCapability,
			matches: func(err error) bool {
				var target *contract.UnsupportedCapabilityError
				return errors.As(err, &target) && target.Capability == "custom-feature-gate"
			},
		},
		{
			name:     "kubernetes evaluation",
			typed:    &contract.KubernetesEvaluationError{Operation: "evaluate selector", Err: cause},
			category: contract.ErrKubernetesEvaluation,
			matches: func(err error) bool {
				var target *contract.KubernetesEvaluationError
				return errors.As(err, &target) && target.Operation == "evaluate selector"
			},
		},
		{
			name:     "internal",
			typed:    &contract.InternalError{Operation: "assemble trace", Err: cause},
			category: contract.ErrInternal,
			matches: func(err error) bool {
				var target *contract.InternalError
				return errors.As(err, &target) && target.Operation == "assemble trace"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := fmt.Errorf("evaluate webhook: %w", test.typed)
			if !test.matches(err) {
				t.Errorf("errors.As(%v, %T) = false, want true", err, test.typed)
			}
			if !errors.Is(err, test.category) {
				t.Errorf("errors.Is(%v, %v) = false, want true", err, test.category)
			}
			if !errors.Is(err, cause) {
				t.Errorf("errors.Is(%v, cause) = false, want true", err)
			}
		})
	}
}

func TestEvaluationErrorNilReceivers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		category error
		unwrap   func() error
	}{
		{
			name:     "invalid input",
			err:      (*contract.InvalidInputError)(nil),
			category: contract.ErrInvalidInput,
			unwrap: func() error {
				return (*contract.InvalidInputError)(nil).Unwrap()
			},
		},
		{
			name:     "missing context",
			err:      (*contract.MissingContextError)(nil),
			category: contract.ErrMissingContext,
			unwrap: func() error {
				return (*contract.MissingContextError)(nil).Unwrap()
			},
		},
		{
			name:     "unsupported capability",
			err:      (*contract.UnsupportedCapabilityError)(nil),
			category: contract.ErrUnsupportedCapability,
			unwrap: func() error {
				return (*contract.UnsupportedCapabilityError)(nil).Unwrap()
			},
		},
		{
			name:     "kubernetes evaluation",
			err:      (*contract.KubernetesEvaluationError)(nil),
			category: contract.ErrKubernetesEvaluation,
			unwrap: func() error {
				return (*contract.KubernetesEvaluationError)(nil).Unwrap()
			},
		},
		{
			name:     "internal",
			err:      (*contract.InternalError)(nil),
			category: contract.ErrInternal,
			unwrap: func() error {
				return (*contract.InternalError)(nil).Unwrap()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_ = test.err.Error()
			if got := test.unwrap(); got != nil {
				t.Errorf("Unwrap() = %v, want nil", got)
			}
			if !errors.Is(test.err, test.category) {
				t.Errorf("errors.Is(nil receiver, %v) = false, want true", test.category)
			}
		})
	}
}

func TestResourceLimitErrorCategoriesAndMessage(t *testing.T) {
	t.Parallel()

	err := &contract.ResourceLimitError{
		Field:    ".request.object",
		Resource: "document nesting depth",
		Actual:   101,
		Limit:    100,
	}
	if !errors.Is(err, contract.ErrResourceLimit) {
		t.Errorf("errors.Is(error, ErrResourceLimit) = false, want true")
	}
	if !errors.Is(err, contract.ErrInvalidInput) {
		t.Errorf("errors.Is(error, ErrInvalidInput) = false, want true")
	}
	want := `resource limit exceeded: document nesting depth: got 101, limit 100 at field ".request.object"`
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
