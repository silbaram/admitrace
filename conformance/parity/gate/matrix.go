// Package gate defines the checked parity coverage matrix and release-gate report contract.
package gate

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/silbaram/admitrace/internal/contract"
)

const (
	// MatrixSchemaVersion identifies the checked coverage matrix contract.
	MatrixSchemaVersion = "admitrace.parity-matrix/v1alpha1"
	// ReportSchemaVersion identifies deterministic parity release-gate reports.
	ReportSchemaVersion = "admitrace.parity-report/v1alpha1"
	// KubernetesOracleVersion is the only oracle release accepted by this gate.
	KubernetesOracleVersion = "1.36.2"
)

//go:embed matrix.json
var matrixJSON []byte

var requiredTags = []string{
	"cel:error",
	"cel:false",
	"cel:none",
	"cel:true",
	"configuration:mutating",
	"configuration:validating",
	"context:missing",
	"failure-policy:fail",
	"failure-policy:ignore",
	"match-policy:equivalent",
	"match-policy:exact",
	"operation:create",
	"operation:delete",
	"operation:update",
	"rules:match",
	"rules:multiple",
	"scope:cluster",
	"scope:namespaced",
	"selector:namespace",
	"selector:object",
	"webhooks:multiple",
}

var validOracleTypes = []string{
	"golden-trace",
	"incomplete-contract",
	"kube-apiserver-observation",
}

var validConfigurationKinds = []string{
	"MutatingWebhookConfiguration",
	"ValidatingWebhookConfiguration",
}

var validSuites = []string{
	"cel-authorizer",
	"core",
	"equivalent-selector",
}

var validOutcomes = []string{
	"called",
	"rejected-before-call",
	"skipped",
}

// Matrix is the complete release-gate coverage declaration.
type Matrix struct {
	SchemaVersion string `json:"schemaVersion"`
	OracleVersion string `json:"oracleVersion"`
	ProfileID     string `json:"profileId"`
	Cases         []Case `json:"cases"`
}

// Case records the support profile, oracle category, and branch tags for one fixture.
type Case struct {
	ID                string   `json:"id"`
	Suite             string   `json:"suite"`
	ProfileID         string   `json:"profileId"`
	OracleType        string   `json:"oracleType"`
	ConfigurationKind string   `json:"configurationKind"`
	ExpectedOutcomes  []string `json:"expectedOutcomes"`
	Tags              []string `json:"tags"`
}

// LoadMatrix decodes and validates the embedded release-gate matrix.
func LoadMatrix() (Matrix, error) {
	return DecodeMatrix(bytes.NewReader(matrixJSON))
}

// DecodeMatrix decodes one strict matrix document and validates its coverage contract.
func DecodeMatrix(reader io.Reader) (Matrix, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var matrix Matrix
	if err := decoder.Decode(&matrix); err != nil {
		return Matrix{}, fmt.Errorf("decode parity coverage matrix: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Matrix{}, err
	}
	if err := matrix.Validate(); err != nil {
		return Matrix{}, err
	}
	return matrix, nil
}

// Validate checks the exact profile, fixture identities, oracle categories, and required tags.
func (matrix Matrix) Validate() error {
	if matrix.SchemaVersion != MatrixSchemaVersion {
		return fmt.Errorf("matrix schemaVersion = %q, want %q", matrix.SchemaVersion, MatrixSchemaVersion)
	}
	profile := contract.Kubernetes136DefaultProfile()
	if matrix.OracleVersion != KubernetesOracleVersion || matrix.OracleVersion != profile.KubernetesVersion {
		return fmt.Errorf("matrix oracleVersion = %q, want %q", matrix.OracleVersion, KubernetesOracleVersion)
	}
	if matrix.ProfileID != profile.ID {
		return fmt.Errorf("matrix profileId = %q, want %q", matrix.ProfileID, profile.ID)
	}
	if len(matrix.Cases) < 20 {
		return fmt.Errorf("matrix case count = %d, want at least 20", len(matrix.Cases))
	}

	seenIDs := make(map[string]struct{}, len(matrix.Cases))
	coveredTags := make(map[string]struct{})
	coveredOracleTypes := make(map[string]struct{})
	coveredOutcomes := make(map[string]struct{})
	coveredSuites := make(map[string]struct{})
	for index, testCase := range matrix.Cases {
		if err := validateCase(index, matrix.ProfileID, testCase); err != nil {
			return err
		}
		if _, found := seenIDs[testCase.ID]; found {
			return fmt.Errorf("duplicate scenario id %q", testCase.ID)
		}
		seenIDs[testCase.ID] = struct{}{}
		coveredOracleTypes[testCase.OracleType] = struct{}{}
		coveredSuites[testCase.Suite] = struct{}{}
		for _, outcome := range testCase.ExpectedOutcomes {
			coveredOutcomes[outcome] = struct{}{}
		}
		for _, tag := range testCase.Tags {
			coveredTags[tag] = struct{}{}
		}
	}

	for _, oracleType := range validOracleTypes {
		if _, found := coveredOracleTypes[oracleType]; !found {
			return fmt.Errorf("matrix oracleType %q is not covered", oracleType)
		}
	}
	for _, outcome := range validOutcomes {
		if _, found := coveredOutcomes[outcome]; !found {
			return fmt.Errorf("matrix outcome %q is not covered", outcome)
		}
	}
	for _, suite := range validSuites {
		if _, found := coveredSuites[suite]; !found {
			return fmt.Errorf("matrix suite %q is not covered", suite)
		}
	}
	for _, tag := range requiredTags {
		if _, found := coveredTags[tag]; !found {
			return fmt.Errorf("matrix coverage tag %q is missing", tag)
		}
	}
	return nil
}

// SHA256 returns the checksum of the exact embedded matrix bytes.
func SHA256() string {
	digest := sha256.Sum256(matrixJSON)
	return hex.EncodeToString(digest[:])
}

// OracleCounts returns deterministic counts keyed by the closed oracle category.
func (matrix Matrix) OracleCounts() map[string]int {
	counts := make(map[string]int, len(validOracleTypes))
	for _, oracleType := range validOracleTypes {
		counts[oracleType] = 0
	}
	for _, testCase := range matrix.Cases {
		counts[testCase.OracleType]++
	}
	return counts
}

// ObservationCount returns the number of API server observation fixtures in one suite.
func (matrix Matrix) ObservationCount(suite string) int {
	count := 0
	for _, testCase := range matrix.Cases {
		if testCase.Suite == suite && testCase.OracleType == "kube-apiserver-observation" {
			count++
		}
	}
	return count
}

// OracleTypeFor returns the declared oracle category for a Scenario id.
func (matrix Matrix) OracleTypeFor(id string) (string, bool) {
	for _, testCase := range matrix.Cases {
		if testCase.ID == id {
			return testCase.OracleType, true
		}
	}
	return "", false
}

func validateCase(index int, profileID string, testCase Case) error {
	if strings.TrimSpace(testCase.ID) == "" {
		return fmt.Errorf("case[%d] id is required", index)
	}
	if !slices.Contains(validSuites, testCase.Suite) {
		return fmt.Errorf("scenario %q suite = %q, want a registered suite", testCase.ID, testCase.Suite)
	}
	if testCase.ProfileID != profileID {
		return fmt.Errorf("scenario %q profileId = %q, want %q", testCase.ID, testCase.ProfileID, profileID)
	}
	if !slices.Contains(validOracleTypes, testCase.OracleType) {
		return fmt.Errorf("scenario %q oracleType = %q, want a registered type", testCase.ID, testCase.OracleType)
	}
	if testCase.Suite == "core" && testCase.OracleType != "kube-apiserver-observation" {
		return fmt.Errorf("scenario %q core suite oracleType = %q, want kube-apiserver-observation", testCase.ID, testCase.OracleType)
	}
	if !slices.Contains(validConfigurationKinds, testCase.ConfigurationKind) {
		return fmt.Errorf("scenario %q configurationKind = %q, want a registered kind", testCase.ID, testCase.ConfigurationKind)
	}
	if testCase.OracleType == "incomplete-contract" && len(testCase.ExpectedOutcomes) != 0 {
		return fmt.Errorf("scenario %q incomplete contract has outcomes", testCase.ID)
	}
	if testCase.OracleType != "incomplete-contract" && len(testCase.ExpectedOutcomes) == 0 {
		return fmt.Errorf("scenario %q determinate oracle outcomes are required", testCase.ID)
	}
	for _, outcome := range testCase.ExpectedOutcomes {
		if !slices.Contains(validOutcomes, outcome) {
			return fmt.Errorf("scenario %q outcome = %q, want a registered outcome", testCase.ID, outcome)
		}
	}
	if len(testCase.Tags) == 0 {
		return fmt.Errorf("scenario %q coverage tags are required", testCase.ID)
	}
	if !slices.IsSorted(testCase.Tags) {
		return fmt.Errorf("scenario %q coverage tags must be sorted", testCase.ID)
	}
	for tagIndex, tag := range testCase.Tags {
		if strings.TrimSpace(tag) == "" {
			return fmt.Errorf("scenario %q coverage tag[%d] is empty", testCase.ID, tagIndex)
		}
		if tagIndex > 0 && testCase.Tags[tagIndex-1] == tag {
			return fmt.Errorf("scenario %q duplicate coverage tag %q", testCase.ID, tag)
		}
	}
	return nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode trailing parity coverage matrix data: %w", err)
	}
	return errors.New("parity coverage matrix contains multiple JSON values")
}
