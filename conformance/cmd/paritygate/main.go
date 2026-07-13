// Command paritygate runs the pinned Kubernetes parity release gate.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/silbaram/admitrace/conformance/parity/gate"
)

const diagnosticLimit = 8192

type options struct {
	goBinary   string
	reportPath string
}

type phase struct {
	name       string
	suite      string
	oracleType string
	testRoot   string
	args       []string
}

type testEvent struct {
	Action string `json:"Action"`
	Test   string `json:"Test"`
}

type oracleCounts struct {
	KubeAPIServerObservation              int `json:"kubeAPIServerObservation"`
	OfficialKubernetesMatcherDifferential int `json:"officialKubernetesMatcherDifferential"`
	IncompleteContract                    int `json:"incompleteContract"`
}

type failureCounts struct {
	SetupFailure                int `json:"setupFailure"`
	SemanticMismatch            int `json:"semanticMismatch"`
	DifferentialContractFailure int `json:"differentialContractFailure"`
	IncompleteContractFailure   int `json:"incompleteContractFailure"`
	OtherContractFailure        int `json:"otherContractFailure"`
}

type phaseResult struct {
	Name              string        `json:"name"`
	Status            string        `json:"status"`
	ExpectedCaseCount int           `json:"expectedCaseCount"`
	ExecutedCaseCount int           `json:"executedCaseCount"`
	ExecutedCaseIDs   []string      `json:"executedCaseIds"`
	FailedCases       []string      `json:"failedCases"`
	FailureCategories []string      `json:"failureCategories"`
	Failures          failureCounts `json:"failures"`
	MissingCaseCounts oracleCounts  `json:"missingCaseCounts"`
}

type summary struct {
	TotalCases       int           `json:"totalCases"`
	OracleTypeCounts oracleCounts  `json:"oracleTypeCounts"`
	FailureCounts    failureCounts `json:"failureCounts"`
}

type caseSummary struct {
	ID         string `json:"id"`
	Suite      string `json:"suite"`
	ProfileID  string `json:"profileId"`
	OracleType string `json:"oracleType"`
}

type report struct {
	SchemaVersion string        `json:"schemaVersion"`
	OracleVersion string        `json:"oracleVersion"`
	ProfileID     string        `json:"profileId"`
	MatrixSHA256  string        `json:"matrixSHA256"`
	Status        string        `json:"status"`
	Summary       summary       `json:"summary"`
	Cases         []caseSummary `json:"cases"`
	Phases        []phaseResult `json:"phases"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "parity gate: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	settings, err := parseOptions(args, stderr)
	if err != nil {
		return err
	}
	matrix, err := gate.LoadMatrix()
	if err != nil {
		return err
	}

	phases := releasePhases()
	report := newReport(matrix)
	failed := false
	for _, current := range phases {
		result, output := executePhase(settings.goBinary, current, matrix)
		report.Phases = append(report.Phases, result)
		addFailures(&report.Summary.FailureCounts, result.Failures)
		if result.Status == "failed" {
			failed = true
			fmt.Fprintf(stderr, "%s failed: %s\n%s", current.name, strings.Join(result.FailureCategories, ","), diagnosticTail(output))
		}
	}
	matrixResult := matrixExecutionResult(report.Phases, matrix, failed)
	report.Phases = append(report.Phases, matrixResult)
	addFailures(&report.Summary.FailureCounts, matrixResult.Failures)
	if matrixResult.Status == "failed" {
		failed = true
		fmt.Fprintf(stderr, "%s failed: %s\n", matrixResult.Name, strings.Join(matrixResult.FailureCategories, ","))
	}
	if failed {
		report.Status = "failed"
	}
	if err := writeReport(settings.reportPath, stdout, report); err != nil {
		return err
	}
	if failed {
		return errors.New("release parity gate failed; inspect the deterministic report")
	}
	return nil
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	flags := flag.NewFlagSet("paritygate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	goBinary := flags.String("go", "go", "Go executable used for parity test groups")
	reportPath := flags.String("report", "-", "report destination, or - for stdout")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(*goBinary) == "" {
		return options{}, errors.New("go executable is required")
	}
	if strings.TrimSpace(*reportPath) == "" {
		return options{}, errors.New("report destination is required")
	}
	return options{goBinary: *goBinary, reportPath: *reportPath}, nil
}

func releasePhases() []phase {
	return []phase{
		{
			name: "offline-contracts",
			args: []string{"test", "-json", "-count=1", "-mod=readonly", "./oracle", "./parity/..."},
		},
		{
			name: "official-kubernetes-matcher-differential", oracleType: "official-kubernetes-matcher-differential",
			testRoot: "TestOfficialMatcherDifferential",
			args:     []string{"test", "-json", "-count=1", "-mod=readonly", "-run", "^TestOfficialMatcherDifferential$", "./parity/equivalentselector", "./parity/celauthorizer"},
		},
		{
			name: "core-kube-apiserver-observations", suite: "core", testRoot: "TestCoreRulesAndSelectorsParity",
			args: []string{"test", "-json", "-count=1", "-mod=readonly", "-tags=conformance", "-run", "^TestCoreRulesAndSelectorsParity$", "./oracle"},
		},
		{
			name: "equivalent-kube-apiserver-observation", suite: "equivalent-selector",
			testRoot: "TestKubeAPIServerObservations",
			args:     []string{"test", "-json", "-count=1", "-mod=readonly", "-tags=conformance", "-run", "^TestKubeAPIServerObservations$", "./parity/equivalentselector"},
		},
		{
			name: "cel-kube-apiserver-observations", suite: "cel-authorizer", testRoot: "TestKubeAPIServerObservations",
			args: []string{"test", "-json", "-count=1", "-mod=readonly", "-tags=conformance", "-run", "^TestKubeAPIServerObservations$", "./parity/celauthorizer"},
		},
	}
}

func executePhase(goBinary string, current phase, matrix gate.Matrix) (phaseResult, []byte) {
	result := phaseResult{
		Name:              current.name,
		Status:            "passed",
		ExpectedCaseCount: expectedCaseCount(current, matrix),
		ExecutedCaseIDs:   []string{},
		FailedCases:       []string{},
		FailureCategories: []string{},
	}
	command := exec.Command(goBinary, current.args...)
	command.Env = replaceEnvironment(os.Environ(), map[string]string{
		"GOPROXY": "off",
		"GOWORK":  "off",
	})
	output, commandErr := command.CombinedOutput()
	events, parseErr := decodeEvents(output)
	result.ExecutedCaseIDs = executedCaseIDs(events, current, matrix)
	result.ExecutedCaseCount = len(result.ExecutedCaseIDs)
	result.FailedCases = failedCases(events)
	result.Failures = classifyFailures(commandErr, output, result.FailedCases, matrix, current.suite == "")

	switch {
	case commandErr != nil:
		result.Status = "failed"
	case parseErr != nil:
		result.Status = "failed"
		result.Failures.OtherContractFailure++
		output = append(output, []byte("\nparse go test JSON: "+parseErr.Error()+"\n")...)
	case result.ExecutedCaseCount != result.ExpectedCaseCount:
		result.Status = "failed"
		result.Failures.OtherContractFailure++
		output = append(output, []byte(fmt.Sprintf(
			"\nexecuted matrix case count got %d, want %d\n",
			result.ExecutedCaseCount,
			result.ExpectedCaseCount,
		))...)
	}
	result.FailureCategories = categories(result.Failures)
	return result, output
}

func decodeEvents(output []byte) ([]testEvent, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var events []testEvent
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event testEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan events: %w", err)
	}
	if len(events) == 0 {
		return nil, errors.New("no go test events were emitted")
	}
	return events, nil
}

func executedCaseIDs(events []testEvent, current phase, matrix gate.Matrix) []string {
	seen := make(map[string]struct{})
	for _, event := range events {
		if event.Action != "run" {
			continue
		}
		if !strings.Contains(event.Test, "/") {
			continue
		}
		parts := strings.Split(event.Test, "/")
		id := parts[len(parts)-1]
		if _, found := matrix.OracleTypeFor(id); found {
			seen[id] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	slices.Sort(result)
	return result
}

func failedCases(events []testEvent) []string {
	failedTests := make(map[string]struct{})
	for _, event := range events {
		if event.Action != "fail" || event.Test == "" {
			continue
		}
		failedTests[event.Test] = struct{}{}
	}
	seen := make(map[string]struct{})
	for testName := range failedTests {
		if !strings.Contains(testName, "/") && hasFailedChild(testName, failedTests) {
			continue
		}
		parts := strings.Split(testName, "/")
		seen[parts[len(parts)-1]] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	slices.Sort(result)
	return result
}

func hasFailedChild(testName string, failedTests map[string]struct{}) bool {
	prefix := testName + "/"
	for candidate := range failedTests {
		if strings.HasPrefix(candidate, prefix) {
			return true
		}
	}
	return false
}

func classifyFailures(commandErr error, output []byte, failed []string, matrix gate.Matrix, contractPhase bool) failureCounts {
	var counts failureCounts
	if commandErr == nil {
		return counts
	}
	if commandStartFailed(commandErr) {
		counts.SetupFailure++
		return counts
	}
	text := string(output)
	if strings.Contains(text, "oracle setup failed at") {
		counts.SetupFailure++
	}
	semanticMarker := strings.Contains(text, "oracle semantic mismatch") || strings.Contains(text, "core parity mismatch")
	if semanticMarker {
		counts.SemanticMismatch += semanticFailureCount(failed, matrix)
	}
	if contractPhase {
		for _, id := range failed {
			oracleType, found := matrix.OracleTypeFor(id)
			if !found {
				counts.OtherContractFailure++
				continue
			}
			if semanticMarker && oracleType == "kube-apiserver-observation" {
				continue
			}
			switch oracleType {
			case "official-kubernetes-matcher-differential":
				counts.DifferentialContractFailure++
			case "incomplete-contract":
				counts.IncompleteContractFailure++
			default:
				counts.OtherContractFailure++
			}
		}
	}
	if commandErr != nil && counts.total() == 0 {
		counts.OtherContractFailure++
	}
	return counts
}

func semanticFailureCount(failed []string, matrix gate.Matrix) int {
	count := 0
	for _, id := range failed {
		oracleType, found := matrix.OracleTypeFor(id)
		if found && oracleType == "kube-apiserver-observation" {
			count++
		}
	}
	return atLeastOne(count)
}

func commandStartFailed(err error) bool {
	if err == nil {
		return false
	}
	var executableError *exec.Error
	var pathError *os.PathError
	return errors.As(err, &executableError) || errors.As(err, &pathError)
}

func expectedCaseCount(current phase, matrix gate.Matrix) int {
	if current.oracleType != "" {
		return matrix.OracleTypeCount(current.oracleType)
	}
	if current.suite != "" {
		return matrix.ObservationCount(current.suite)
	}
	count := 0
	for _, testCase := range matrix.Cases {
		if testCase.Suite != "core" {
			count++
		}
	}
	return count
}

func matrixExecutionResult(phases []phaseResult, matrix gate.Matrix, prerequisiteFailed bool) phaseResult {
	result := phaseResult{
		Name:              "matrix-execution-coverage",
		Status:            "passed",
		ExpectedCaseCount: len(matrix.Cases),
		ExecutedCaseIDs:   []string{},
		FailedCases:       []string{},
		FailureCategories: []string{},
	}
	seen := make(map[string]struct{})
	for _, phase := range phases {
		for _, id := range phase.ExecutedCaseIDs {
			seen[id] = struct{}{}
		}
	}
	for id := range seen {
		result.ExecutedCaseIDs = append(result.ExecutedCaseIDs, id)
	}
	slices.Sort(result.ExecutedCaseIDs)
	result.ExecutedCaseCount = len(result.ExecutedCaseIDs)
	for _, testCase := range matrix.Cases {
		if _, found := seen[testCase.ID]; !found {
			result.FailedCases = append(result.FailedCases, testCase.ID)
			addMissingCaseCount(&result.MissingCaseCounts, testCase.OracleType)
			if !prerequisiteFailed {
				addMissingCaseFailure(&result.Failures, testCase.OracleType)
			}
		}
	}
	slices.Sort(result.FailedCases)
	if prerequisiteFailed {
		result.Status = "not-run"
		return result
	}
	if len(result.FailedCases) > 0 {
		result.Status = "failed"
		result.FailureCategories = categories(result.Failures)
	}
	return result
}

func addMissingCaseCount(counts *oracleCounts, oracleType string) {
	switch oracleType {
	case "official-kubernetes-matcher-differential":
		counts.OfficialKubernetesMatcherDifferential++
	case "incomplete-contract":
		counts.IncompleteContract++
	default:
		counts.KubeAPIServerObservation++
	}
}

func addMissingCaseFailure(counts *failureCounts, oracleType string) {
	switch oracleType {
	case "official-kubernetes-matcher-differential":
		counts.DifferentialContractFailure++
	case "incomplete-contract":
		counts.IncompleteContractFailure++
	default:
		counts.OtherContractFailure++
	}
}

func addFailures(total *failureCounts, add failureCounts) {
	total.SetupFailure += add.SetupFailure
	total.SemanticMismatch += add.SemanticMismatch
	total.DifferentialContractFailure += add.DifferentialContractFailure
	total.IncompleteContractFailure += add.IncompleteContractFailure
	total.OtherContractFailure += add.OtherContractFailure
}

func categories(counts failureCounts) []string {
	result := []string{}
	if counts.SetupFailure > 0 {
		result = append(result, "setup-failure")
	}
	if counts.SemanticMismatch > 0 {
		result = append(result, "semantic-mismatch")
	}
	if counts.DifferentialContractFailure > 0 {
		result = append(result, "differential-contract-failure")
	}
	if counts.IncompleteContractFailure > 0 {
		result = append(result, "incomplete-contract-failure")
	}
	if counts.OtherContractFailure > 0 {
		result = append(result, "other-contract-failure")
	}
	return result
}

func (counts failureCounts) total() int {
	return counts.SetupFailure + counts.SemanticMismatch + counts.DifferentialContractFailure + counts.IncompleteContractFailure + counts.OtherContractFailure
}

func newReport(matrix gate.Matrix) report {
	counts := matrix.OracleCounts()
	result := report{
		SchemaVersion: gate.ReportSchemaVersion,
		OracleVersion: matrix.OracleVersion,
		ProfileID:     matrix.ProfileID,
		MatrixSHA256:  gate.SHA256(),
		Status:        "passed",
		Cases:         make([]caseSummary, 0, len(matrix.Cases)),
		Phases:        []phaseResult{},
		Summary: summary{
			TotalCases: len(matrix.Cases),
			OracleTypeCounts: oracleCounts{
				KubeAPIServerObservation:              counts["kube-apiserver-observation"],
				OfficialKubernetesMatcherDifferential: counts["official-kubernetes-matcher-differential"],
				IncompleteContract:                    counts["incomplete-contract"],
			},
		},
	}
	for _, testCase := range matrix.Cases {
		result.Cases = append(result.Cases, caseSummary{
			ID:         testCase.ID,
			Suite:      testCase.Suite,
			ProfileID:  testCase.ProfileID,
			OracleType: testCase.OracleType,
		})
	}
	slices.SortFunc(result.Cases, func(left, right caseSummary) int {
		return strings.Compare(left.ID, right.ID)
	})
	return result
}

func writeReport(path string, stdout io.Writer, value report) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode parity report: %w", err)
	}
	data = append(data, '\n')
	if path == "-" {
		if _, err := stdout.Write(data); err != nil {
			return fmt.Errorf("write parity report: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(filepath.Clean(path), data, 0o644); err != nil {
		return fmt.Errorf("write parity report %s: %w", path, err)
	}
	return nil
}

func replaceEnvironment(current []string, replacements map[string]string) []string {
	result := make([]string, 0, len(current)+len(replacements))
	for _, entry := range current {
		name, _, found := strings.Cut(entry, "=")
		if found {
			if _, replace := replacements[name]; replace {
				continue
			}
		}
		result = append(result, entry)
	}
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		result = append(result, key+"="+replacements[key])
	}
	return result
}

func diagnosticTail(output []byte) string {
	if len(output) <= diagnosticLimit {
		return string(output)
	}
	return string(output[len(output)-diagnosticLimit:])
}

func atLeastOne(value int) int {
	if value > 0 {
		return value
	}
	return 1
}
