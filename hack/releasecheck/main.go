// Command releasecheck validates deterministic parity and beta evidence for a release.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const (
	paritySchema = "admitrace.parity-report/v1alpha1"
	betaSchema   = "admitrace.beta-validation/v1alpha1"
)

type failureCounts struct {
	SetupFailure                int `json:"setupFailure"`
	SemanticMismatch            int `json:"semanticMismatch"`
	DifferentialContractFailure int `json:"differentialContractFailure"`
	IncompleteContractFailure   int `json:"incompleteContractFailure"`
	OtherContractFailure        int `json:"otherContractFailure"`
}

type oracleCounts struct {
	KubeAPIServerObservation              int `json:"kubeAPIServerObservation"`
	OfficialKubernetesMatcherDifferential int `json:"officialKubernetesMatcherDifferential"`
	IncompleteContract                    int `json:"incompleteContract"`
}

type parityReport struct {
	SchemaVersion string `json:"schemaVersion"`
	OracleVersion string `json:"oracleVersion"`
	ProfileID     string `json:"profileId"`
	Status        string `json:"status"`
	Summary       struct {
		TotalCases       int           `json:"totalCases"`
		OracleTypeCounts oracleCounts  `json:"oracleTypeCounts"`
		FailureCounts    failureCounts `json:"failureCounts"`
	} `json:"summary"`
	Cases []struct {
		ID         string `json:"id"`
		OracleType string `json:"oracleType"`
	} `json:"cases"`
	Phases []struct {
		Name              string `json:"name"`
		Status            string `json:"status"`
		ExpectedCaseCount int    `json:"expectedCaseCount"`
		ExecutedCaseCount int    `json:"executedCaseCount"`
	} `json:"phases"`
}

type betaReport struct {
	SchemaVersion string `json:"schemaVersion"`
	Summary       struct {
		Projects           int `json:"projects"`
		Scenarios          int `json:"scenarios"`
		Webhooks           int `json:"webhooks"`
		Determinate        int `json:"determinate"`
		Incomplete         int `json:"incomplete"`
		SemanticMismatches int `json:"semanticMismatches"`
	} `json:"summary"`
}

func main() {
	parityPath := flag.String("parity", "", "path to the parity report")
	betaPath := flag.String("beta", "", "path to the beta report")
	flag.Parse()
	if flag.NArg() != 0 || *parityPath == "" || *betaPath == "" {
		fmt.Fprintln(os.Stderr, "release evidence: -parity and -beta are required and positional arguments are not accepted")
		os.Exit(2)
	}

	var parity parityReport
	if err := decodeJSONFile(*parityPath, &parity); err != nil {
		fmt.Fprintf(os.Stderr, "release evidence: %v\n", err)
		os.Exit(1)
	}
	if err := validateParity(parity); err != nil {
		fmt.Fprintf(os.Stderr, "release evidence: %v\n", err)
		os.Exit(1)
	}

	var beta betaReport
	if err := decodeJSONFile(*betaPath, &beta); err != nil {
		fmt.Fprintf(os.Stderr, "release evidence: %v\n", err)
		os.Exit(1)
	}
	if err := validateBeta(beta); err != nil {
		fmt.Fprintf(os.Stderr, "release evidence: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf(
		"release evidence: passed (%d parity cases: 21 direct, 8 official matcher differential, 4 incomplete; 0 mismatches; %d beta projects)\n",
		parity.Summary.TotalCases,
		beta.Summary.Projects,
	)
}

func decodeJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing %s data: %w", path, err)
	}
	return fmt.Errorf("decode %s: multiple JSON values", path)
}

func validateParity(report parityReport) error {
	if report.SchemaVersion != paritySchema {
		return fmt.Errorf("parity schemaVersion = %q, want %q", report.SchemaVersion, paritySchema)
	}
	if report.OracleVersion != "1.36.2" || report.ProfileID != "kubernetes-1.36.2-defaults" {
		return fmt.Errorf("parity oracle/profile = %q/%q, want 1.36.2/kubernetes-1.36.2-defaults", report.OracleVersion, report.ProfileID)
	}
	if report.Status != "passed" {
		return fmt.Errorf("parity status = %q, want passed", report.Status)
	}
	if report.Summary.TotalCases != 33 || report.Summary.TotalCases != len(report.Cases) {
		return fmt.Errorf("parity case count = summary %d, cases %d; want 33", report.Summary.TotalCases, len(report.Cases))
	}
	if report.Summary.FailureCounts != (failureCounts{}) {
		return fmt.Errorf("parity failure counts = %+v, want all zero", report.Summary.FailureCounts)
	}

	caseIDs := make(map[string]struct{}, len(report.Cases))
	actualOracleCounts := oracleCounts{}
	for _, testCase := range report.Cases {
		if testCase.ID == "" {
			return errors.New("parity case id is empty")
		}
		if _, found := caseIDs[testCase.ID]; found {
			return fmt.Errorf("parity case id %q is duplicated", testCase.ID)
		}
		caseIDs[testCase.ID] = struct{}{}
		switch testCase.OracleType {
		case "kube-apiserver-observation":
			actualOracleCounts.KubeAPIServerObservation++
		case "official-kubernetes-matcher-differential":
			actualOracleCounts.OfficialKubernetesMatcherDifferential++
		case "incomplete-contract":
			actualOracleCounts.IncompleteContract++
		default:
			return fmt.Errorf("parity case %q oracleType = %q, want an approved independent or incomplete oracle", testCase.ID, testCase.OracleType)
		}
	}
	wantOracleCounts := oracleCounts{
		KubeAPIServerObservation:              21,
		OfficialKubernetesMatcherDifferential: 8,
		IncompleteContract:                    4,
	}
	if report.Summary.OracleTypeCounts != wantOracleCounts || actualOracleCounts != wantOracleCounts {
		return fmt.Errorf("parity oracle coverage = summary %+v, cases %+v; want %+v", report.Summary.OracleTypeCounts, actualOracleCounts, wantOracleCounts)
	}

	requiredPhases := []struct {
		name  string
		count int
	}{
		{name: "offline-contracts", count: 20},
		{name: "official-kubernetes-matcher-differential", count: 8},
		{name: "core-kube-apiserver-observations", count: 13},
		{name: "equivalent-kube-apiserver-observation", count: 2},
		{name: "cel-kube-apiserver-observations", count: 6},
		{name: "matrix-execution-coverage", count: 33},
	}
	requiredPhaseCounts := make(map[string]int, len(requiredPhases))
	for _, requirement := range requiredPhases {
		requiredPhaseCounts[requirement.name] = requirement.count
	}
	seenPhases := make(map[string]bool, len(requiredPhases))
	for _, phase := range report.Phases {
		if phase.Status != "passed" || phase.ExpectedCaseCount != phase.ExecutedCaseCount {
			return fmt.Errorf("parity phase %q = status %q, executed %d/%d", phase.Name, phase.Status, phase.ExecutedCaseCount, phase.ExpectedCaseCount)
		}
		if expected, required := requiredPhaseCounts[phase.Name]; required {
			if seenPhases[phase.Name] {
				return fmt.Errorf("parity phase %q is duplicated", phase.Name)
			}
			seenPhases[phase.Name] = true
			if phase.ExecutedCaseCount != expected {
				return fmt.Errorf("parity phase %q executed %d cases, want %d", phase.Name, phase.ExecutedCaseCount, expected)
			}
		}
	}
	for _, requirement := range requiredPhases {
		if !seenPhases[requirement.name] {
			return fmt.Errorf("parity phase %q is missing", requirement.name)
		}
	}
	return nil
}

func validateBeta(report betaReport) error {
	if report.SchemaVersion != betaSchema {
		return fmt.Errorf("beta schemaVersion = %q, want %q", report.SchemaVersion, betaSchema)
	}
	if report.Summary.Projects != 2 || report.Summary.Scenarios != 2 {
		return fmt.Errorf("beta projects/scenarios = %d/%d, want 2/2", report.Summary.Projects, report.Summary.Scenarios)
	}
	if report.Summary.Webhooks != 6 || report.Summary.Determinate != 6 {
		return fmt.Errorf("beta webhooks/determinate = %d/%d, want 6/6", report.Summary.Webhooks, report.Summary.Determinate)
	}
	if report.Summary.Incomplete != 0 || report.Summary.SemanticMismatches != 0 {
		return fmt.Errorf("beta incomplete/mismatches = %d/%d, want 0/0", report.Summary.Incomplete, report.Summary.SemanticMismatches)
	}
	return nil
}
