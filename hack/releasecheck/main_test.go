package main

import (
	"strings"
	"testing"
)

func TestValidateParity(t *testing.T) {
	valid := validParityReport()
	tests := []struct {
		name   string
		mutate func(*parityReport)
		want   string
	}{
		{name: "valid"},
		{name: "too few cases", mutate: func(report *parityReport) {
			report.Cases = report.Cases[:19]
			report.Summary.TotalCases = 19
		}, want: "at least 20"},
		{name: "semantic mismatch", mutate: func(report *parityReport) {
			report.Summary.FailureCounts.SemanticMismatch = 1
		}, want: "want all zero"},
		{name: "phase false pass", mutate: func(report *parityReport) {
			report.Phases[0].ExecutedCaseCount--
		}, want: "executed"},
		{name: "missing phase", mutate: func(report *parityReport) {
			report.Phases = report.Phases[1:]
		}, want: "is missing"},
		{name: "duplicate case", mutate: func(report *parityReport) {
			report.Cases[1].ID = report.Cases[0].ID
		}, want: "is duplicated"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := valid
			report.Cases = append([]struct {
				ID string `json:"id"`
			}{}, valid.Cases...)
			report.Phases = append([]struct {
				Name              string `json:"name"`
				Status            string `json:"status"`
				ExpectedCaseCount int    `json:"expectedCaseCount"`
				ExecutedCaseCount int    `json:"executedCaseCount"`
			}{}, valid.Phases...)
			if test.mutate != nil {
				test.mutate(&report)
			}
			err := validateParity(report)
			if test.want == "" {
				if err != nil {
					t.Fatalf("validateParity() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateParity() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateBeta(t *testing.T) {
	report := betaReport{SchemaVersion: betaSchema}
	report.Summary.Projects = 2
	report.Summary.Scenarios = 2
	report.Summary.Webhooks = 6
	report.Summary.Determinate = 6
	if err := validateBeta(report); err != nil {
		t.Fatalf("validateBeta() error = %v, want nil", err)
	}

	report.Summary.SemanticMismatches = 1
	if err := validateBeta(report); err == nil || !strings.Contains(err.Error(), "want 0/0") {
		t.Fatalf("validateBeta() error = %v, want mismatch failure", err)
	}
}

func validParityReport() parityReport {
	report := parityReport{
		SchemaVersion: paritySchema,
		OracleVersion: "1.36.2",
		ProfileID:     "kubernetes-1.36.2-defaults",
		Status:        "passed",
	}
	report.Summary.TotalCases = 20
	for index := range 20 {
		report.Cases = append(report.Cases, struct {
			ID string `json:"id"`
		}{ID: "case-" + string(rune('a'+index))})
	}
	for _, name := range []string{
		"offline-contracts",
		"core-kube-apiserver-observations",
		"equivalent-kube-apiserver-observation",
		"cel-kube-apiserver-observations",
		"matrix-execution-coverage",
	} {
		count := 1
		if name == "matrix-execution-coverage" {
			count = report.Summary.TotalCases
		}
		report.Phases = append(report.Phases, struct {
			Name              string `json:"name"`
			Status            string `json:"status"`
			ExpectedCaseCount int    `json:"expectedCaseCount"`
			ExecutedCaseCount int    `json:"executedCaseCount"`
		}{Name: name, Status: "passed", ExpectedCaseCount: count, ExecutedCaseCount: count})
	}
	return report
}
