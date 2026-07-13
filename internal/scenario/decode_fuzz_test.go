package scenario_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/scenario"
)

func FuzzDecoderDeterministicClassification(f *testing.F) {
	valid := scenarioDocument(validatingConfigurationDocument("", ""), "", "")
	f.Add([]byte(valid))
	f.Add([]byte("apiVersion: admitrace.io/v1alpha1\nkind: Scenario\n"))
	f.Add([]byte(`{"apiVersion":`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		first, firstErr := scenario.Decode(data)
		second, secondErr := scenario.Decode(data)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("Decode() error presence = (%v, %v), want equal", firstErr, secondErr)
		}
		if firstErr != nil {
			if firstErr.Error() != secondErr.Error() {
				t.Fatalf("Decode() errors = (%q, %q), want equal", firstErr, secondErr)
			}
			for _, category := range []error{
				contract.ErrInvalidInput,
				contract.ErrResourceLimit,
				contract.ErrUnsupportedCapability,
			} {
				firstMatches := errors.Is(firstErr, category)
				secondMatches := errors.Is(secondErr, category)
				if firstMatches != secondMatches {
					t.Fatalf("Decode() second category %v = %t, want %t", category, secondMatches, firstMatches)
				}
			}
			return
		}
		equal := reflect.DeepEqual(first, second)
		if !equal {
			t.Fatalf("reflect.DeepEqual(Decode() second result, first result) = %t, want true", equal)
		}
	})
}
