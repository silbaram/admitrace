package main

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/conformance/parity/gate"
)

func TestDecodeEventsAndCaseAccounting(t *testing.T) {
	input := strings.Join([]string{
		`{"Action":"run","Test":"TestKubeAPIServerObservations/cel-true"}`,
		`{"Action":"pass","Test":"TestKubeAPIServerObservations/cel-true"}`,
		`{"Action":"run","Test":"TestKubeAPIServerObservations/cel-false"}`,
		`{"Action":"fail","Test":"TestKubeAPIServerObservations/cel-false"}`,
	}, "\n")
	events, err := decodeEvents([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	current := phase{suite: "cel-authorizer", testRoot: "TestKubeAPIServerObservations"}
	matrix, err := gate.LoadMatrix()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(executedCaseIDs(events, current, matrix), ","), "cel-false,cel-true"; got != want {
		t.Errorf("executed cases = %q, want %q", got, want)
	}
	failed := failedCases(events)
	if got, want := strings.Join(failed, ","), "cel-false"; got != want {
		t.Errorf("failed cases = %q, want %q", got, want)
	}
}

func TestFailuresAreCountedWithoutCollapsingCategories(t *testing.T) {
	matrix, err := gate.LoadMatrix()
	if err != nil {
		t.Fatal(err)
	}
	output := []byte("oracle setup failed at tls\ncore parity mismatch case=x\n")
	counts := classifyFailures(
		errors.New("exit status 1"),
		output,
		[]string{"cel-compile-fail", "authorizer-missing", "unknown-subtest"},
		matrix,
		true,
	)
	if got, want := counts.SetupFailure, 1; got != want {
		t.Errorf("setup failures = %d, want %d", got, want)
	}
	if got, want := counts.SemanticMismatch, 1; got != want {
		t.Errorf("semantic mismatches = %d, want %d", got, want)
	}
	if got, want := counts.DifferentialContractFailure, 1; got != want {
		t.Errorf("differential contract failures = %d, want %d", got, want)
	}
	if got, want := counts.IncompleteContractFailure, 1; got != want {
		t.Errorf("incomplete contract failures = %d, want %d", got, want)
	}
	if got, want := counts.OtherContractFailure, 1; got != want {
		t.Errorf("other contract failures = %d, want %d", got, want)
	}
}

func TestFailedCasesPreservesIndependentRootFailure(t *testing.T) {
	events := []testEvent{
		{Action: "fail", Test: "TestParity/cel-compile-fail"},
		{Action: "fail", Test: "TestParity"},
		{Action: "fail", Test: "TestIndependentRootFailure"},
	}
	if got, want := strings.Join(failedCases(events), ","), "TestIndependentRootFailure,cel-compile-fail"; got != want {
		t.Errorf("failed cases = %q, want %q", got, want)
	}
}

func TestCommandStartFailureIsSetupFailure(t *testing.T) {
	matrix, err := gate.LoadMatrix()
	if err != nil {
		t.Fatal(err)
	}
	counts := classifyFailures(&exec.Error{Name: "missing-go", Err: exec.ErrNotFound}, nil, nil, matrix, false)
	if got, want := counts.SetupFailure, 1; got != want {
		t.Errorf("setup failures = %d, want %d", got, want)
	}
	if got := counts.total(); got != 1 {
		t.Errorf("total failures = %d, want 1", got)
	}
}

func TestMatrixExecutionResultRejectsMissingCase(t *testing.T) {
	matrix, err := gate.LoadMatrix()
	if err != nil {
		t.Fatal(err)
	}
	executed := make([]string, 0, len(matrix.Cases)-1)
	for _, testCase := range matrix.Cases {
		if testCase.ID != "authorizer-missing" {
			executed = append(executed, testCase.ID)
		}
	}
	result := matrixExecutionResult([]phaseResult{{ExecutedCaseIDs: executed}}, matrix, false)
	if got, want := result.Status, "failed"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got, want := strings.Join(result.FailedCases, ","), "authorizer-missing"; got != want {
		t.Errorf("failed cases = %q, want %q", got, want)
	}
	if got, want := result.Failures.IncompleteContractFailure, 1; got != want {
		t.Errorf("incomplete contract failures = %d, want %d", got, want)
	}
}

func TestMatrixExecutionResultReportsMissingCasesAfterSetupFailure(t *testing.T) {
	matrix, err := gate.LoadMatrix()
	if err != nil {
		t.Fatal(err)
	}
	result := matrixExecutionResult(nil, matrix, true)
	if got, want := result.Status, "not-run"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got, want := len(result.FailedCases), len(matrix.Cases); got != want {
		t.Errorf("missing case count = %d, want %d", got, want)
	}
	if got, want := result.MissingCaseCounts.KubeAPIServerObservation, 21; got != want {
		t.Errorf("missing API server observations = %d, want %d", got, want)
	}
	if got := result.Failures.total(); got != 0 {
		t.Errorf("contract failures = %d, want 0 after setup failure", got)
	}
}

func TestReportEncodingIsDeterministic(t *testing.T) {
	matrix, err := gate.LoadMatrix()
	if err != nil {
		t.Fatal(err)
	}
	value := newReport(matrix)
	value.Phases = []phaseResult{{Name: "offline-contracts", Status: "passed", ExecutedCaseIDs: []string{}, FailedCases: []string{}, FailureCategories: []string{}}}
	var first bytes.Buffer
	if err := writeReport("-", &first, value); err != nil {
		t.Fatal(err)
	}
	var second bytes.Buffer
	if err := writeReport("-", &second, value); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("report bytes differ:\nfirst=%s\nsecond=%s", first.Bytes(), second.Bytes())
	}
	for _, forbidden := range []string{"timestamp", "/private/", "duration"} {
		if bytes.Contains(first.Bytes(), []byte(forbidden)) {
			t.Errorf("report contains nondeterministic field %q", forbidden)
		}
	}
}
