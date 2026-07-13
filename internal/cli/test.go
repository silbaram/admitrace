package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	"github.com/silbaram/admitrace/internal/render"
	"github.com/silbaram/admitrace/internal/scenario"
	"github.com/spf13/cobra"
)

const testReportVersion = "admitrace.test/v1alpha1"

type testReport struct {
	SchemaVersion string          `json:"schemaVersion"`
	Fixtures      []fixtureReport `json:"fixtures"`
	Summary       testSummary     `json:"summary"`
}

type fixtureReport struct {
	Path         string                     `json:"path"`
	ScenarioID   string                     `json:"scenarioId,omitempty"`
	Status       string                     `json:"status"`
	Expectations []expectationComparison    `json:"expectations,omitempty"`
	Evaluation   *contract.EvaluationResult `json:"evaluation,omitempty"`
	Error        string                     `json:"error,omitempty"`
	code         ExitCode
}

type expectationComparison struct {
	WebhookName string                `json:"webhookName"`
	Matched     bool                  `json:"matched"`
	Expected    expectedWebhookResult `json:"expected"`
	Actual      actualWebhookResult   `json:"actual"`
	Mismatches  []expectationMismatch `json:"mismatches"`
}

type expectedWebhookResult struct {
	Determination      contract.Determination `json:"determination"`
	Outcome            *contract.Outcome      `json:"outcome,omitempty"`
	TerminalReasonCode contract.ReasonCode    `json:"terminalReasonCode,omitempty"`
}

type actualWebhookResult struct {
	Determination      contract.Determination `json:"determination"`
	Outcome            *contract.Outcome      `json:"outcome,omitempty"`
	TerminalReasonCode contract.ReasonCode    `json:"terminalReasonCode,omitempty"`
}

type expectationMismatch struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type testSummary struct {
	Total      int      `json:"total"`
	Passed     int      `json:"passed"`
	Mismatched int      `json:"mismatched"`
	Invalid    int      `json:"invalid"`
	Incomplete int      `json:"incomplete"`
	Internal   int      `json:"internal"`
	ExitCode   ExitCode `json:"exitCode"`
}

type discoveredScenario struct {
	path string
	err  error
}

func newTestCommand(output *string, exitCode *ExitCode) *cobra.Command {
	command := &cobra.Command{
		Use:   "test <path>...",
		Short: "Test Scenario expectations",
		Long: "Test Scenario expectations from files and directories. Explicit files are read regardless of extension. " +
			"Directories are searched recursively for regular .yaml, .yml, and .json files without following symlink directories. " +
			"Duplicate clean paths are evaluated once, and all discovered paths are evaluated in lexical order.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, paths []string) error {
			format, err := parseOutputFormat(*output)
			if err != nil {
				return newCommandError(ExitInvalidInput, true, err)
			}

			report := runScenarioTests(command.Context(), paths)
			content, err := renderTestReport(report, format)
			if err != nil {
				return internalError("render test report", err)
			}
			if err := writeOutput(command.OutOrStdout(), content); err != nil {
				return internalError("write test output", err)
			}
			*exitCode = report.Summary.ExitCode
			return nil
		},
	}
	return command
}

func runScenarioTests(ctx context.Context, paths []string) testReport {
	report := testReport{
		SchemaVersion: testReportVersion,
		Fixtures:      []fixtureReport{},
	}
	for _, discovered := range discoverScenarios(paths) {
		fixture := evaluateFixture(ctx, discovered)
		report.Fixtures = append(report.Fixtures, fixture)
	}
	report.Summary = summarizeTestReport(report.Fixtures)
	return report
}

func discoverScenarios(inputs []string) []discoveredScenario {
	discovered := make(map[string]discoveredScenario)
	documentCount := 0
	for _, input := range inputs {
		path := filepath.Clean(input)
		info, err := os.Stat(path)
		if err != nil {
			discovered[path] = discoveredScenario{path: path, err: fmt.Errorf("inspect path: %w", err)}
			continue
		}
		if !info.IsDir() {
			if info.Mode().IsRegular() {
				if _, exists := discovered[path]; !exists {
					documentCount++
					if err := scenario.CheckDocumentCount(documentCount); err != nil {
						return []discoveredScenario{{path: path, err: err}}
					}
				}
				discovered[path] = discoveredScenario{path: path}
			} else {
				discovered[path] = discoveredScenario{path: path, err: fmt.Errorf("path is not a regular file or directory")}
			}
			continue
		}
		if err := discoverDirectory(path, discovered, &documentCount); err != nil {
			return []discoveredScenario{{path: path, err: err}}
		}
	}

	result := make([]discoveredScenario, 0, len(discovered))
	for _, entry := range discovered {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].path < result[j].path })
	return result
}

func discoverDirectory(root string, discovered map[string]discoveredScenario, documentCount *int) error {
	found := false
	walkFailed := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		path = filepath.Clean(path)
		if walkErr != nil {
			walkFailed = true
			discovered[path] = discoveredScenario{path: path, err: fmt.Errorf("walk directory: %w", walkErr)}
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		if !isScenarioExtension(filepath.Ext(path)) {
			return nil
		}
		found = true
		if _, exists := discovered[path]; !exists {
			*documentCount++
			if err := scenario.CheckDocumentCount(*documentCount); err != nil {
				return err
			}
		}
		discovered[path] = discoveredScenario{path: path}
		return nil
	})
	if err != nil {
		if errors.Is(err, contract.ErrResourceLimit) {
			return err
		}
		discovered[root] = discoveredScenario{path: root, err: fmt.Errorf("walk directory: %w", err)}
		return nil
	}
	if !found && !walkFailed {
		discovered[root] = discoveredScenario{path: root, err: fmt.Errorf("directory contains no .yaml, .yml, or .json Scenario files")}
	}
	return nil
}

func isScenarioExtension(extension string) bool {
	switch strings.ToLower(extension) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}

func evaluateFixture(ctx context.Context, discovered discoveredScenario) fixtureReport {
	if discovered.err != nil {
		return failedFixture(discovered.path, ExitInvalidInput, discovered.err)
	}

	data, err := readScenario(nil, discovered.path)
	if err != nil {
		return failedFixture(discovered.path, classifyTestError(err), err)
	}
	input, err := scenario.Decode(data)
	if err != nil {
		return failedFixture(discovered.path, classifyTestError(err), fmt.Errorf("decode Scenario: %w", err))
	}
	snapshot, err := evaluation.SnapshotFromScenario(*input)
	if err != nil {
		return failedFixture(discovered.path, classifyTestError(err), fmt.Errorf("prepare Scenario: %w", err))
	}

	result := evaluation.NewEvaluator().Evaluate(ctx, snapshot)
	canonical, err := canonicalEvaluation(result)
	if err != nil {
		return failedFixture(discovered.path, ExitInternalError, fmt.Errorf("canonicalize evaluation: %w", err))
	}
	comparisons, comparisonCodes := compareExpectations(input.Expectations, canonical.Webhooks)
	codes := append([]ExitCode{diagnosticExitCode(canonical.Diagnostics)}, comparisonCodes...)
	for _, webhook := range canonical.Webhooks {
		codes = append(codes, diagnosticExitCode(webhook.Diagnostics))
	}
	code := SelectExitCode(codes...)
	return fixtureReport{
		Path:         discovered.path,
		ScenarioID:   input.Metadata.Name,
		Status:       testStatus(code),
		Expectations: comparisons,
		Evaluation:   &canonical,
		code:         code,
	}
}

func failedFixture(path string, code ExitCode, err error) fixtureReport {
	return fixtureReport{
		Path:   path,
		Status: testStatus(code),
		Error:  err.Error(),
		code:   code,
	}
}

func classifyTestError(err error) ExitCode {
	if errors.Is(err, contract.ErrInvalidInput) {
		return ExitInvalidInput
	}
	return ExitInternalError
}

func canonicalEvaluation(result contract.EvaluationResult) (contract.EvaluationResult, error) {
	data, err := render.JSON(result)
	if err != nil {
		return contract.EvaluationResult{}, err
	}
	var canonical contract.EvaluationResult
	if err := json.Unmarshal(data, &canonical); err != nil {
		return contract.EvaluationResult{}, &contract.InternalError{Operation: "decode canonical evaluation", Err: err}
	}
	return canonical, nil
}

func compareExpectations(expectations []contract.WebhookExpectation, webhooks []contract.WebhookEvaluation) ([]expectationComparison, []ExitCode) {
	byName := make(map[string]contract.WebhookExpectation, len(expectations))
	for _, expectation := range expectations {
		byName[expectation.WebhookName] = expectation
	}

	comparisons := make([]expectationComparison, 0, len(expectations))
	codes := make([]ExitCode, 0, len(webhooks))
	for _, webhook := range webhooks {
		expectation, asserted := byName[webhook.WebhookName]
		if !asserted {
			if webhook.Determination != contract.DeterminationDeterminate {
				codes = append(codes, ExitIncompleteEvaluation)
			}
			continue
		}

		comparison := compareExpectation(expectation, webhook)
		comparisons = append(comparisons, comparison)
		if !comparison.Matched {
			codes = append(codes, ExitExpectationMismatch)
		}
		if webhook.Determination != contract.DeterminationDeterminate && webhook.Determination != expectation.Determination {
			codes = append(codes, ExitIncompleteEvaluation)
		}
	}
	return comparisons, codes
}

func compareExpectation(expectation contract.WebhookExpectation, webhook contract.WebhookEvaluation) expectationComparison {
	terminalReason := terminalReasonCode(webhook.Trace)
	comparison := expectationComparison{
		WebhookName: expectation.WebhookName,
		Expected: expectedWebhookResult{
			Determination:      expectation.Determination,
			Outcome:            expectation.Outcome,
			TerminalReasonCode: expectation.TerminalReasonCode,
		},
		Actual: actualWebhookResult{
			Determination:      webhook.Determination,
			Outcome:            webhook.Outcome,
			TerminalReasonCode: terminalReason,
		},
		Mismatches: []expectationMismatch{},
	}
	if expectation.Determination != webhook.Determination {
		comparison.Mismatches = append(comparison.Mismatches, expectationMismatch{
			Field: "determination", Expected: string(expectation.Determination), Actual: string(webhook.Determination),
		})
	}
	if expectation.Outcome != nil && !equalOutcomes(expectation.Outcome, webhook.Outcome) {
		comparison.Mismatches = append(comparison.Mismatches, expectationMismatch{
			Field: "outcome", Expected: string(*expectation.Outcome), Actual: optionalOutcome(webhook.Outcome),
		})
	}
	if expectation.TerminalReasonCode != "" && expectation.TerminalReasonCode != terminalReason {
		comparison.Mismatches = append(comparison.Mismatches, expectationMismatch{
			Field: "terminalReasonCode", Expected: string(expectation.TerminalReasonCode), Actual: optionalReason(terminalReason),
		})
	}
	comparison.Matched = len(comparison.Mismatches) == 0
	return comparison
}

func terminalReasonCode(trace []contract.TraceStep) contract.ReasonCode {
	for i := len(trace) - 1; i >= 0; i-- {
		if trace[i].Terminal {
			return trace[i].ReasonCode
		}
	}
	return ""
}

func equalOutcomes(left, right *contract.Outcome) bool {
	return left != nil && right != nil && *left == *right
}

func optionalOutcome(outcome *contract.Outcome) string {
	if outcome == nil {
		return "<absent>"
	}
	return string(*outcome)
}

func summarizeTestReport(fixtures []fixtureReport) testSummary {
	summary := testSummary{Total: len(fixtures)}
	codes := make([]ExitCode, 0, len(fixtures))
	for _, fixture := range fixtures {
		codes = append(codes, fixture.code)
		switch fixture.code {
		case ExitSuccess:
			summary.Passed++
		case ExitExpectationMismatch:
			summary.Mismatched++
		case ExitInvalidInput:
			summary.Invalid++
		case ExitIncompleteEvaluation:
			summary.Incomplete++
		case ExitInternalError:
			summary.Internal++
		}
	}
	summary.ExitCode = SelectExitCode(codes...)
	return summary
}

func testStatus(code ExitCode) string {
	switch code {
	case ExitSuccess:
		return "passed"
	case ExitExpectationMismatch:
		return "mismatched"
	case ExitInvalidInput:
		return "invalid"
	case ExitIncompleteEvaluation:
		return "incomplete"
	default:
		return "internal-error"
	}
}

func renderTestReport(report testReport, format outputFormat) ([]byte, error) {
	switch format {
	case outputJSON:
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, &contract.InternalError{Operation: "marshal test report", Err: err}
		}
		return append(data, '\n'), nil
	case outputText:
		return renderTestText(report)
	default:
		return nil, fmt.Errorf("unsupported output format %q", format)
	}
}

func renderTestText(report testReport) ([]byte, error) {
	var output bytes.Buffer
	fmt.Fprintf(&output, "schemaVersion: %s\n", report.SchemaVersion)
	output.WriteString("fixtures:\n")
	for _, fixture := range report.Fixtures {
		fmt.Fprintf(&output, "  - path: %s\n", strconv.Quote(fixture.Path))
		fmt.Fprintf(&output, "    scenario: %s\n", strconv.Quote(fixture.ScenarioID))
		fmt.Fprintf(&output, "    status: %s\n", fixture.Status)
		if fixture.Error != "" {
			fmt.Fprintf(&output, "    error: %s\n", strconv.Quote(fixture.Error))
		}
		writeExpectationText(&output, fixture.Expectations)
		if fixture.Evaluation != nil {
			data, err := render.Text(*fixture.Evaluation)
			if err != nil {
				return nil, fmt.Errorf("render evaluation for %q: %w", fixture.Path, err)
			}
			output.WriteString("    evaluation:\n")
			writeIndented(&output, data, 6)
		}
	}
	fmt.Fprintf(&output, "summary: total=%d passed=%d mismatched=%d invalid=%d incomplete=%d internal=%d exitCode=%d\n",
		report.Summary.Total,
		report.Summary.Passed,
		report.Summary.Mismatched,
		report.Summary.Invalid,
		report.Summary.Incomplete,
		report.Summary.Internal,
		report.Summary.ExitCode,
	)
	return output.Bytes(), nil
}

func writeExpectationText(output *bytes.Buffer, comparisons []expectationComparison) {
	if len(comparisons) == 0 {
		output.WriteString("    expectations: []\n")
		return
	}
	output.WriteString("    expectations:\n")
	for _, comparison := range comparisons {
		fmt.Fprintf(output, "      - webhook: %s\n", strconv.Quote(comparison.WebhookName))
		fmt.Fprintf(output, "        matched: %t\n", comparison.Matched)
		fmt.Fprintf(output, "        expected: determination=%s outcome=%s terminalReasonCode=%s\n",
			comparison.Expected.Determination,
			optionalOutcome(comparison.Expected.Outcome),
			optionalReason(comparison.Expected.TerminalReasonCode),
		)
		fmt.Fprintf(output, "        actual: determination=%s outcome=%s terminalReasonCode=%s\n",
			comparison.Actual.Determination,
			optionalOutcome(comparison.Actual.Outcome),
			optionalReason(comparison.Actual.TerminalReasonCode),
		)
		for _, mismatch := range comparison.Mismatches {
			fmt.Fprintf(output, "        mismatch: %s expected=%s actual=%s\n",
				mismatch.Field,
				strconv.Quote(mismatch.Expected),
				strconv.Quote(mismatch.Actual),
			)
		}
	}
}

func optionalReason(reason contract.ReasonCode) string {
	if reason == "" {
		return "<absent>"
	}
	return string(reason)
}

func writeIndented(output *bytes.Buffer, data []byte, spaces int) {
	prefix := strings.Repeat(" ", spaces)
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		fmt.Fprintf(output, "%s%s\n", prefix, line)
	}
}
