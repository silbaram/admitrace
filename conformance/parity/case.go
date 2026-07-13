package parity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	"github.com/silbaram/admitrace/internal/scenario"
)

// OracleType identifies the independent evidence used for one parity case.
type OracleType string

const (
	// OracleKubeAPIServerObservation uses a call, skip, or pre-call rejection
	// observed from a Kubernetes 1.36.2 API server.
	OracleKubeAPIServerObservation OracleType = "kube-apiserver-observation"
	// OracleOfficialMatcherDifferential compares the product decision with the
	// official Kubernetes 1.36.2 matcher in the test-only conformance module.
	OracleOfficialMatcherDifferential OracleType = "official-kubernetes-matcher-differential"
	// OracleIncompleteContract records behavior that cannot produce an outcome
	// without fixture context or unsupported semantics.
	OracleIncompleteContract OracleType = "incomplete-contract"
)

// TraceExpectation is the stable subset of one trace step used as oracle data.
type TraceExpectation struct {
	Stage      string
	Result     contract.TraceResult
	ReasonCode contract.ReasonCode
	Pending    bool
	Discarded  bool
	Terminal   bool
}

// ExpectedResult is the product result required by a parity case.
type ExpectedResult struct {
	Determination contract.Determination
	Outcome       *contract.Outcome
	ReasonCode    contract.ReasonCode
	Trace         []TraceExpectation
	Diagnostics   []contract.ReasonCode
}

// Case is one independent Scenario with exactly one declared oracle category.
type Case struct {
	Group           string
	Scenario        contract.Scenario
	OracleType      OracleType
	OracleRationale string
	CoverageTags    []string
	Expected        ExpectedResult
}

// Evaluate applies defaults and runs one Scenario against the production
// snapshot evaluator. Tests that inject a bounded compatibility seam can use
// CompareEvaluation directly with their resulting evaluation.
func Evaluate(ctx context.Context, testCase Case) (contract.WebhookEvaluation, error) {
	snapshot, err := BuildSnapshot(testCase)
	if err != nil {
		return contract.WebhookEvaluation{}, err
	}
	return EvaluateSnapshot(ctx, testCase.Scenario.Metadata.Name, snapshot)
}

// BuildSnapshot validates and prepares one fixture snapshot. Tests may replace
// a bounded provider seam after validation to inject a recorded lookup result.
func BuildSnapshot(testCase Case) (evaluation.Snapshot, error) {
	input := testCase.Scenario
	scenario.ApplyDefaults(&input)
	if err := scenario.Validate(&input); err != nil {
		return evaluation.Snapshot{}, fmt.Errorf("validate scenario %q: %w", input.Metadata.Name, err)
	}
	snapshot, err := evaluation.SnapshotFromScenario(input)
	if err != nil {
		return evaluation.Snapshot{}, fmt.Errorf("build scenario %q snapshot: %w", input.Metadata.Name, err)
	}
	return snapshot, nil
}

// EvaluateSnapshot evaluates a prepared snapshot and requires one webhook.
func EvaluateSnapshot(ctx context.Context, name string, snapshot evaluation.Snapshot) (contract.WebhookEvaluation, error) {
	result := evaluation.NewEvaluator().Evaluate(ctx, snapshot)
	if len(result.Webhooks) != 1 {
		return contract.WebhookEvaluation{}, fmt.Errorf("scenario %q produced %d webhook results, want 1", name, len(result.Webhooks))
	}
	return result.Webhooks[0], nil
}

// ValidateCases checks fixture identity, closed oracle categories, stable tags,
// and the absent-outcome invariant for incomplete cases.
func ValidateCases(group string, cases []Case) error {
	if strings.TrimSpace(group) == "" {
		return errors.New("group is required")
	}
	if len(cases) == 0 {
		return fmt.Errorf("group %q has no cases", group)
	}
	seen := make(map[string]struct{}, len(cases))
	for index, testCase := range cases {
		name := testCase.Scenario.Metadata.Name
		if testCase.Group != group {
			return fmt.Errorf("case[%d] group = %q, want %q", index, testCase.Group, group)
		}
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("case[%d] Scenario name is required", index)
		}
		if _, found := seen[name]; found {
			return fmt.Errorf("duplicate Scenario name %q", name)
		}
		seen[name] = struct{}{}
		if !testCase.OracleType.valid() {
			return fmt.Errorf("scenario %q oracleType = %q, want a registered type", name, testCase.OracleType)
		}
		if strings.TrimSpace(testCase.OracleRationale) == "" {
			return fmt.Errorf("scenario %q oracle rationale is required", name)
		}
		if err := validateTags(name, testCase.CoverageTags); err != nil {
			return err
		}
		if err := validateExpectedResult(name, testCase.Expected); err != nil {
			return err
		}
		if testCase.OracleType == OracleOfficialMatcherDifferential && len(testCase.Expected.Trace) == 0 {
			return fmt.Errorf("scenario %q supplemental golden trace is required", name)
		}
		if testCase.OracleType == OracleIncompleteContract {
			if testCase.Expected.Determination == contract.DeterminationDeterminate {
				return fmt.Errorf("scenario %q incomplete determination must not be determinate", name)
			}
			if testCase.Expected.Outcome != nil {
				return fmt.Errorf("scenario %q incomplete outcome must be nil", name)
			}
			if len(testCase.Expected.Diagnostics) == 0 || len(testCase.Expected.Trace) == 0 {
				return fmt.Errorf("scenario %q incomplete diagnostic and trace are required", name)
			}
			if err := validateIncompleteEvidence(name, testCase.Expected); err != nil {
				return err
			}
		}
	}
	return nil
}

// CompareEvaluation compares validated product output with the declared stable,
// typed oracle subset. It never includes diagnostic messages or input summaries.
func CompareEvaluation(testCase Case, actual contract.WebhookEvaluation) error {
	if err := actual.Validate(); err != nil {
		return fmt.Errorf("validate actual evaluation: %w", err)
	}
	var mismatches []string
	want := testCase.Expected
	if actual.Determination != want.Determination {
		mismatches = append(mismatches, fmt.Sprintf("determination got %q, want %q", actual.Determination, want.Determination))
	}
	if !equalOutcome(actual.Outcome, want.Outcome) {
		mismatches = append(mismatches, fmt.Sprintf("outcome got %s, want %s", formatOutcome(actual.Outcome), formatOutcome(want.Outcome)))
	}
	if reason := terminalReason(actual.Trace); reason != want.ReasonCode {
		mismatches = append(mismatches, fmt.Sprintf("terminal reason got %q, want %q", reason, want.ReasonCode))
	}
	if want.Trace != nil {
		gotTrace := traceExpectations(actual.Trace)
		if !slices.Equal(gotTrace, want.Trace) {
			mismatches = append(mismatches, fmt.Sprintf("trace got %#v, want %#v", gotTrace, want.Trace))
		}
	}
	gotDiagnostics := diagnosticCodes(actual.Diagnostics)
	if !slices.Equal(gotDiagnostics, want.Diagnostics) {
		mismatches = append(mismatches, fmt.Sprintf("diagnostics got %q, want %q", gotDiagnostics, want.Diagnostics))
	}
	if len(mismatches) == 0 {
		return nil
	}
	return fmt.Errorf("scenario %q parity mismatch: %s", testCase.Scenario.Metadata.Name, strings.Join(mismatches, "; "))
}

// CoverageReport returns one deterministic line per Scenario for CI logs.
func CoverageReport(cases []Case) []string {
	rows := make([]string, len(cases))
	for index, testCase := range cases {
		rows[index] = fmt.Sprintf("scenario=%s oracleType=%s tags=%s rationale=%s", testCase.Scenario.Metadata.Name, testCase.OracleType, strings.Join(testCase.CoverageTags, ","), testCase.OracleRationale)
	}
	slices.Sort(rows)
	return rows
}

func (oracleType OracleType) valid() bool {
	switch oracleType {
	case OracleKubeAPIServerObservation, OracleOfficialMatcherDifferential, OracleIncompleteContract:
		return true
	default:
		return false
	}
}

func validateTags(name string, tags []string) error {
	if len(tags) == 0 {
		return fmt.Errorf("scenario %q coverage tags are required", name)
	}
	if !slices.IsSorted(tags) {
		return fmt.Errorf("scenario %q coverage tags must be sorted", name)
	}
	for index, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			return fmt.Errorf("scenario %q coverage tag[%d] is empty", name, index)
		}
		if index > 0 && tags[index-1] == tag {
			return fmt.Errorf("scenario %q duplicate coverage tag %q", name, tag)
		}
	}
	return nil
}

func validateExpectedResult(name string, expected ExpectedResult) error {
	expectation := contract.WebhookExpectation{
		Determination:      expected.Determination,
		Outcome:            expected.Outcome,
		TerminalReasonCode: expected.ReasonCode,
	}
	if err := expectation.Validate(); err != nil {
		return fmt.Errorf("scenario %q expected result: %w", name, err)
	}
	for index, expectedTrace := range expected.Trace {
		if strings.TrimSpace(expectedTrace.Stage) == "" {
			return fmt.Errorf("scenario %q expected trace[%d] stage is required", name, index)
		}
		step := contract.TraceStep{
			Stage:      expectedTrace.Stage,
			Result:     expectedTrace.Result,
			ReasonCode: expectedTrace.ReasonCode,
			Pending:    expectedTrace.Pending,
			Discarded:  expectedTrace.Discarded,
			Terminal:   expectedTrace.Terminal,
		}
		if err := step.Validate(); err != nil {
			return fmt.Errorf("scenario %q expected trace[%d]: %w", name, index, err)
		}
	}
	for index, code := range expected.Diagnostics {
		if !code.IsRegistered() {
			return fmt.Errorf("scenario %q expected diagnostic[%d] code %q is not registered", name, index, code)
		}
	}
	return nil
}

func validateIncompleteEvidence(name string, expected ExpectedResult) error {
	terminalCount := 0
	for _, step := range expected.Trace {
		if !step.Terminal {
			continue
		}
		terminalCount++
		if step.ReasonCode != expected.ReasonCode {
			return fmt.Errorf("scenario %q incomplete terminal reason = %q, want %q", name, step.ReasonCode, expected.ReasonCode)
		}
		if expected.Determination == contract.DeterminationIndeterminate && step.Result != contract.TraceResultIndeterminate {
			return fmt.Errorf("scenario %q incomplete terminal result = %q, want %q", name, step.Result, contract.TraceResultIndeterminate)
		}
		if expected.Determination == contract.DeterminationUnsupported && step.Result != contract.TraceResultUnsupported {
			return fmt.Errorf("scenario %q incomplete terminal result = %q, want %q", name, step.Result, contract.TraceResultUnsupported)
		}
	}
	if terminalCount != 1 {
		return fmt.Errorf("scenario %q incomplete terminal trace count = %d, want 1", name, terminalCount)
	}
	if !slices.Contains(expected.Diagnostics, expected.ReasonCode) {
		return fmt.Errorf("scenario %q incomplete diagnostics must include terminal reason %q", name, expected.ReasonCode)
	}
	return nil
}

func traceExpectations(trace []contract.TraceStep) []TraceExpectation {
	result := make([]TraceExpectation, len(trace))
	for index, step := range trace {
		result[index] = TraceExpectation{
			Stage:      step.Stage,
			Result:     step.Result,
			ReasonCode: step.ReasonCode,
			Pending:    step.Pending,
			Discarded:  step.Discarded,
			Terminal:   step.Terminal,
		}
	}
	return result
}

func diagnosticCodes(diagnostics []contract.Diagnostic) []contract.ReasonCode {
	result := make([]contract.ReasonCode, len(diagnostics))
	for index, diagnostic := range diagnostics {
		result[index] = diagnostic.Code
	}
	return result
}

func terminalReason(trace []contract.TraceStep) contract.ReasonCode {
	for index := len(trace) - 1; index >= 0; index-- {
		if trace[index].Terminal {
			return trace[index].ReasonCode
		}
	}
	return ""
}

func equalOutcome(left, right *contract.Outcome) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func formatOutcome(value *contract.Outcome) string {
	if value == nil {
		return "nil"
	}
	return fmt.Sprintf("%q", *value)
}
