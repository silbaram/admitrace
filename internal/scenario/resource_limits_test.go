package scenario_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/scenario"
)

func TestResourceLimitsRejectOversizedAndDeepDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        []byte
		wantResource string
		wantActual   int
		wantLimit    int
	}{
		{
			name:         "encoded bytes",
			input:        []byte(strings.Repeat("x", scenario.MaximumDocumentBytes+1)),
			wantResource: "document bytes",
			wantActual:   scenario.MaximumDocumentBytes + 1,
			wantLimit:    scenario.MaximumDocumentBytes,
		},
		{
			name:         "JSON depth",
			input:        []byte(strings.Repeat(`{"value":`, scenario.MaximumDocumentDepth+1) + `null` + strings.Repeat(`}`, scenario.MaximumDocumentDepth+1)),
			wantResource: "document nesting depth",
			wantActual:   scenario.MaximumDocumentDepth + 1,
			wantLimit:    scenario.MaximumDocumentDepth,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, first := scenario.Decode(test.input)
			_, second := scenario.Decode(test.input)
			if !errors.Is(first, contract.ErrInvalidInput) || !errors.Is(first, contract.ErrResourceLimit) {
				t.Fatalf("Decode() error = %v, want invalid resource limit", first)
			}
			var limit *contract.ResourceLimitError
			if !errors.As(first, &limit) {
				t.Fatalf("Decode() error type = %T, want *contract.ResourceLimitError", first)
			}
			if limit.Resource != test.wantResource || limit.Actual != test.wantActual || limit.Limit != test.wantLimit {
				t.Errorf("ResourceLimitError = %#v, want resource/actual/limit (%q, %d, %d)", limit, test.wantResource, test.wantActual, test.wantLimit)
			}
			if second == nil || second.Error() != first.Error() {
				t.Errorf("repeated error = %v, want %q", second, first.Error())
			}
		})
	}
}

func TestResourceLimitsRejectDocumentCount(t *testing.T) {
	t.Parallel()

	err := scenario.CheckDocumentCount(scenario.MaximumScenarioDocuments + 1)
	if !errors.Is(err, contract.ErrInvalidInput) || !errors.Is(err, contract.ErrResourceLimit) {
		t.Fatalf("CheckDocumentCount() error = %v, want invalid resource limit", err)
	}
	var limit *contract.ResourceLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("CheckDocumentCount() error type = %T, want *contract.ResourceLimitError", err)
	}
	if limit.Resource != "Scenario documents" || limit.Limit != scenario.MaximumScenarioDocuments {
		t.Errorf("ResourceLimitError = %#v, want Scenario document limit %d", limit, scenario.MaximumScenarioDocuments)
	}
}

func TestResourceLimitsAcceptLongLineWithinByteLimit(t *testing.T) {
	t.Parallel()

	input := scenarioDocument(validatingConfigurationDocument("", ""), "", "")
	input += strings.Repeat(" ", 128*1024)
	if len(input) >= scenario.MaximumDocumentBytes {
		t.Fatalf("test input bytes = %d, want less than %d", len(input), scenario.MaximumDocumentBytes)
	}
	if _, err := scenario.Decode([]byte(input)); err != nil {
		t.Errorf("Decode() error = %v, want long line within byte limit accepted", err)
	}
}
