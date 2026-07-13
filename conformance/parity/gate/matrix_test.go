package gate

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmbeddedMatrix(t *testing.T) {
	matrix, err := LoadMatrix()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(matrix.Cases), 33; got != want {
		t.Fatalf("case count = %d, want %d", got, want)
	}
	if got, want := matrix.ObservationCount("core"), 13; got != want {
		t.Errorf("core observation count = %d, want %d", got, want)
	}
	if got, want := matrix.ObservationCount("equivalent-selector"), 2; got != want {
		t.Errorf("equivalent observation count = %d, want %d", got, want)
	}
	if got, want := matrix.ObservationCount("cel-authorizer"), 6; got != want {
		t.Errorf("CEL observation count = %d, want %d", got, want)
	}
	wantOracleCounts := map[string]int{
		"incomplete-contract":                      4,
		"kube-apiserver-observation":               21,
		"official-kubernetes-matcher-differential": 8,
	}
	for oracleType, want := range wantOracleCounts {
		if got := matrix.OracleCounts()[oracleType]; got != want {
			t.Errorf("oracle count %q = %d, want %d", oracleType, got, want)
		}
	}
}

func TestValidateRejectsCoverageContractViolations(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Matrix)
		want string
	}{
		{name: "duplicate id", edit: func(matrix *Matrix) { matrix.Cases[1].ID = matrix.Cases[0].ID }, want: "duplicate scenario id"},
		{name: "profile mismatch", edit: func(matrix *Matrix) { matrix.Cases[0].ProfileID = "kubernetes-1.36.3-defaults" }, want: "profileId"},
		{name: "unknown suite", edit: func(matrix *Matrix) { matrix.Cases[0].Suite = "detached" }, want: "registered suite"},
		{name: "missing tag", edit: func(matrix *Matrix) {
			for index := range matrix.Cases {
				matrix.Cases[index].Tags = removeTag(matrix.Cases[index].Tags, "failure-policy:ignore")
			}
		}, want: "coverage tag \"failure-policy:ignore\" is missing"},
		{name: "open oracle", edit: func(matrix *Matrix) { matrix.Cases[0].OracleType = "external" }, want: "registered type"},
		{name: "golden trace is supplemental only", edit: func(matrix *Matrix) { matrix.Cases[0].OracleType = "golden-trace" }, want: "registered type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matrix := cloneMatrix(t)
			test.edit(&matrix)
			if err := matrix.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func cloneMatrix(t *testing.T) Matrix {
	t.Helper()
	matrix, err := LoadMatrix()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(matrix)
	if err != nil {
		t.Fatal(err)
	}
	cloned, err := DecodeMatrix(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return cloned
}

func removeTag(tags []string, remove string) []string {
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag != remove {
			result = append(result, tag)
		}
	}
	return result
}
