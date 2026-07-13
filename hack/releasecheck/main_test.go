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
		}, want: "want 33"},
		{name: "semantic mismatch", mutate: func(report *parityReport) {
			report.Summary.FailureCounts.SemanticMismatch = 1
		}, want: "want all zero"},
		{name: "phase false pass", mutate: func(report *parityReport) {
			report.Phases[0].ExecutedCaseCount--
		}, want: "executed"},
		{name: "missing phase", mutate: func(report *parityReport) {
			report.Phases = report.Phases[1:]
		}, want: "is missing"},
		{name: "multiple missing phases are deterministic", mutate: func(report *parityReport) {
			report.Phases = report.Phases[2:]
		}, want: `phase "offline-contracts" is missing`},
		{name: "duplicate case", mutate: func(report *parityReport) {
			report.Cases[1].ID = report.Cases[0].ID
		}, want: "is duplicated"},
		{name: "unapproved oracle", mutate: func(report *parityReport) {
			report.Cases[0].OracleType = "golden-trace"
		}, want: "approved independent"},
		{name: "oracle summary false pass", mutate: func(report *parityReport) {
			report.Summary.OracleTypeCounts.KubeAPIServerObservation--
		}, want: "oracle coverage"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := valid
			report.Cases = append([]struct {
				ID         string `json:"id"`
				OracleType string `json:"oracleType"`
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
	report.Summary.TotalCases = 33
	report.Summary.OracleTypeCounts = oracleCounts{
		KubeAPIServerObservation:              21,
		OfficialKubernetesMatcherDifferential: 8,
		IncompleteContract:                    4,
	}
	for index := range 33 {
		oracleType := "incomplete-contract"
		if index < 21 {
			oracleType = "kube-apiserver-observation"
		} else if index < 29 {
			oracleType = "official-kubernetes-matcher-differential"
		}
		report.Cases = append(report.Cases, struct {
			ID         string `json:"id"`
			OracleType string `json:"oracleType"`
		}{ID: "case-" + string(rune('a'+index)), OracleType: oracleType})
	}
	for _, entry := range []struct {
		name  string
		count int
	}{
		{name: "offline-contracts", count: 20},
		{name: "official-kubernetes-matcher-differential", count: 8},
		{name: "core-kube-apiserver-observations", count: 13},
		{name: "equivalent-kube-apiserver-observation", count: 2},
		{name: "cel-kube-apiserver-observations", count: 6},
		{name: "matrix-execution-coverage", count: 33},
	} {
		report.Phases = append(report.Phases, struct {
			Name              string `json:"name"`
			Status            string `json:"status"`
			ExpectedCaseCount int    `json:"expectedCaseCount"`
			ExecutedCaseCount int    `json:"executedCaseCount"`
		}{Name: entry.name, Status: "passed", ExpectedCaseCount: entry.count, ExecutedCaseCount: entry.count})
	}
	return report
}
