package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/manifest"
)

// ManifestJSON returns canonical resource-mode JSON. A single resource is one
// versioned ManifestExplanation object; multiple resources are an ordered
// array of independently versioned objects.
func ManifestJSON(explanations []manifest.ManifestExplanation) ([]byte, error) {
	canonical, err := canonicalManifestExplanations(explanations)
	if err != nil {
		return nil, fmt.Errorf("render manifest JSON: %w", err)
	}
	var data []byte
	if len(canonical) == 1 {
		data, err = json.MarshalIndent(canonical[0], "", "  ")
	} else {
		data, err = json.MarshalIndent(canonical, "", "  ")
	}
	if err != nil {
		return nil, &contract.InternalError{Operation: "marshal manifest explanation", Err: err}
	}
	return append(data, '\n'), nil
}

// ManifestText returns a deterministic human-readable view of resource-mode
// provenance, completeness, diagnostics, and unchanged evaluator results.
func ManifestText(explanations []manifest.ManifestExplanation) ([]byte, error) {
	canonical, err := canonicalManifestExplanations(explanations)
	if err != nil {
		return nil, fmt.Errorf("render manifest text: %w", err)
	}
	var output bytes.Buffer
	for index, explanation := range canonical {
		if index > 0 {
			output.WriteString("---\n")
		}
		if err := writeManifestExplanation(&output, explanation); err != nil {
			return nil, err
		}
	}
	output.WriteString("note: called means routing selected the webhook; no HTTP request was sent and no webhook response or allow/deny was observed.\n")
	return output.Bytes(), nil
}

func canonicalManifestExplanations(explanations []manifest.ManifestExplanation) ([]manifest.ManifestExplanation, error) {
	if len(explanations) == 0 {
		return nil, &contract.InvalidInputError{Field: "manifestExplanations", Err: fmt.Errorf("at least one resource explanation is required")}
	}
	result := make([]manifest.ManifestExplanation, len(explanations))
	for index, explanation := range explanations {
		if err := explanation.Validate(); err != nil {
			return nil, &contract.InvalidInputError{Field: fmt.Sprintf("manifestExplanations[%d]", index), Err: err}
		}
		result[index] = explanation
		result[index].Diagnostics = sortedManifestDiagnostics(explanation.Diagnostics)
		if explanation.Evaluations != nil {
			result[index].Evaluations = make([]manifest.ConfigurationEvaluation, len(explanation.Evaluations))
			for evaluationIndex, evaluation := range explanation.Evaluations {
				canonicalResult, err := canonicalResult(evaluation.Result)
				if err != nil {
					return nil, &contract.InvalidInputError{Field: fmt.Sprintf("manifestExplanations[%d].evaluations[%d].result", index, evaluationIndex), Err: err}
				}
				result[index].Evaluations[evaluationIndex] = evaluation
				result[index].Evaluations[evaluationIndex].Result = canonicalResult
			}
		}
	}
	return result, nil
}

func sortedManifestDiagnostics(input []manifest.Diagnostic) []manifest.Diagnostic {
	if input == nil {
		return nil
	}
	result := append([]manifest.Diagnostic(nil), input...)
	sort.SliceStable(result, func(i, j int) bool {
		left := []string{result[i].SourceLabel, string(result[i].Code), string(result[i].Severity), result[i].Message}
		right := []string{result[j].SourceLabel, string(result[j].Code), string(result[j].Severity), result[j].Message}
		if left[0] == right[0] && result[i].DocumentIndex != result[j].DocumentIndex {
			return result[i].DocumentIndex < result[j].DocumentIndex
		}
		for index := range left {
			if left[index] != right[index] {
				return left[index] < right[index]
			}
		}
		return false
	})
	return result
}

func writeManifestExplanation(output *bytes.Buffer, explanation manifest.ManifestExplanation) error {
	fmt.Fprintf(output, "schemaVersion: %s\n", explanation.SchemaVersion)
	fmt.Fprintf(output, "resourceSource: %s\n", sourceText(explanation.Resource))
	fmt.Fprintf(output, "profileMatch: %s\n", explanation.ProfileMatch.Status)
	fmt.Fprintf(output, "compatibilityProfile: %s\n", explanation.ProfileMatch.Profile.ID)
	fmt.Fprintf(output, "kubernetesVersion: %s\n", explanation.ProfileMatch.Profile.KubernetesVersion)
	if explanation.ProfileMatch.ObservedKubernetesVersion != "" {
		fmt.Fprintf(output, "observedKubernetesVersion: %s\n", explanation.ProfileMatch.ObservedKubernetesVersion)
	}
	output.WriteString("contextCompleteness:\n")
	writeContextStatus(output, "configuration", explanation.ContextCompleteness.Configuration)
	writeContextStatus(output, "discovery", explanation.ContextCompleteness.Discovery)
	writeContextStatus(output, "namespace", explanation.ContextCompleteness.Namespace)
	writeContextStatus(output, "identity", explanation.ContextCompleteness.Identity)
	writeContextStatus(output, "equivalence", explanation.ContextCompleteness.Equivalence)
	writeContextStatus(output, "authorization", explanation.ContextCompleteness.Authorization)
	writeManifestDiagnostics(output, explanation.Diagnostics)
	if len(explanation.Evaluations) == 0 {
		output.WriteString("configurationEvaluations: []\n")
		return nil
	}
	output.WriteString("configurationEvaluations:\n")
	for index, evaluation := range explanation.Evaluations {
		fmt.Fprintf(output, "  - ordinal: %d\n", index+1)
		fmt.Fprintf(output, "    source: %s\n", sourceText(evaluation.Configuration))
		output.WriteString("    result:\n")
		resultText, err := Text(evaluation.Result)
		if err != nil {
			return &contract.InternalError{Operation: "render validated configuration evaluation", Err: err}
		}
		writeIndented(output, resultText, 6)
	}
	return nil
}

func sourceText(source manifest.Source) string {
	description := strconv.Quote(source.Label)
	if source.DocumentIndex > 0 {
		description += fmt.Sprintf(" document %d", source.DocumentIndex)
	}
	return description + " (" + string(source.Kind) + ")"
}

func writeContextStatus(output *bytes.Buffer, label string, status manifest.ContextStatus) {
	fmt.Fprintf(output, "  %s: %s", label, status.Status)
	if status.SourceLabel != "" {
		fmt.Fprintf(output, " (source %s)", strconv.Quote(status.SourceLabel))
	}
	output.WriteByte('\n')
}

func writeManifestDiagnostics(output *bytes.Buffer, diagnostics []manifest.Diagnostic) {
	if len(diagnostics) == 0 {
		output.WriteString("adapterDiagnostics: []\n")
		return
	}
	output.WriteString("adapterDiagnostics:\n")
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(output, "  - code: %s\n", diagnostic.Code)
		fmt.Fprintf(output, "    severity: %s\n", diagnostic.Severity)
		fmt.Fprintf(output, "    message: %s\n", strconv.Quote(diagnostic.Message))
		if diagnostic.SourceLabel != "" {
			fmt.Fprintf(output, "    source: %s", strconv.Quote(diagnostic.SourceLabel))
			if diagnostic.DocumentIndex > 0 {
				fmt.Fprintf(output, " document %d", diagnostic.DocumentIndex)
			}
			output.WriteByte('\n')
		}
	}
}

func writeIndented(output *bytes.Buffer, data []byte, spaces int) {
	prefix := strings.Repeat(" ", spaces)
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		output.WriteString(prefix)
		output.WriteString(line)
		output.WriteByte('\n')
	}
}
