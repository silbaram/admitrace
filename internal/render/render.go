package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/silbaram/admitrace/internal/contract"
)

// JSON returns an indented canonical JSON document terminated by one newline.
// Webhook and trace order are preserved. Diagnostics are sorted by source path,
// code, severity, message, missing-context detail, and unsupported-capability
// detail. Nil and empty collections remain distinct.
func JSON(result contract.EvaluationResult) ([]byte, error) {
	canonical, err := canonicalResult(result)
	if err != nil {
		return nil, fmt.Errorf("render canonical JSON: %w", err)
	}

	data, err := json.MarshalIndent(canonical, "", "  ")
	if err != nil {
		return nil, &contract.InternalError{Operation: "marshal canonical result", Err: err}
	}
	return append(data, '\n'), nil
}

// Text returns a deterministic human-readable view derived from the same
// validated canonical result used by JSON.
func Text(result contract.EvaluationResult) ([]byte, error) {
	canonical, err := canonicalResult(result)
	if err != nil {
		return nil, fmt.Errorf("render text: %w", err)
	}

	var output bytes.Buffer
	fmt.Fprintf(&output, "schemaVersion: %s\n", canonical.SchemaVersion)
	fmt.Fprintf(&output, "scenario: %s\n", strconv.Quote(canonical.ScenarioID))
	fmt.Fprintf(&output, "compatibilityProfile: %s\n", canonical.CompatibilityProfile.ID)
	fmt.Fprintf(&output, "kubernetesVersion: %s\n", canonical.CompatibilityProfile.KubernetesVersion)
	fmt.Fprintf(&output, "featureGatePolicy: %s\n", canonical.CompatibilityProfile.FeatureGatePolicy)
	fmt.Fprintf(&output, "evaluationPhase: %s\n", canonical.EvaluationPhase)
	fmt.Fprintf(&output, "configurationKind: %s\n", canonical.ConfigurationKind)
	writeWebhooks(&output, canonical.Webhooks)
	writeDiagnostics(&output, "diagnostics", canonical.Diagnostics, 0)
	return output.Bytes(), nil
}

func canonicalResult(result contract.EvaluationResult) (contract.EvaluationResult, error) {
	if err := result.Validate(); err != nil {
		return contract.EvaluationResult{}, &contract.InvalidInputError{Field: "result", Err: err}
	}

	canonical := result
	canonical.Diagnostics = sortedDiagnostics(result.Diagnostics)
	if result.Webhooks == nil {
		canonical.Webhooks = nil
	} else {
		canonical.Webhooks = make([]contract.WebhookEvaluation, len(result.Webhooks))
		for i, evaluation := range result.Webhooks {
			canonical.Webhooks[i] = evaluation
			canonical.Webhooks[i].Diagnostics = sortedDiagnostics(evaluation.Diagnostics)
		}
	}
	return canonical, nil
}

func sortedDiagnostics(input []contract.Diagnostic) []contract.Diagnostic {
	if input == nil {
		return nil
	}
	result := append([]contract.Diagnostic(nil), input...)
	sort.SliceStable(result, func(i, j int) bool {
		return compareDiagnostic(result[i], result[j]) < 0
	})
	return result
}

func compareDiagnostic(left, right contract.Diagnostic) int {
	if comparison := compareStrings(left.SourcePath, right.SourcePath); comparison != 0 {
		return comparison
	}
	if comparison := compareStrings(string(left.Code), string(right.Code)); comparison != 0 {
		return comparison
	}
	if comparison := compareStrings(string(left.Severity), string(right.Severity)); comparison != 0 {
		return comparison
	}
	if comparison := compareStrings(left.Message, right.Message); comparison != 0 {
		return comparison
	}
	if comparison := compareMissingContext(left.MissingContext, right.MissingContext); comparison != 0 {
		return comparison
	}
	return compareUnsupportedCapability(left.UnsupportedCapability, right.UnsupportedCapability)
}

func compareMissingContext(left, right *contract.MissingContextDetail) int {
	if comparison := comparePresence(left != nil, right != nil); comparison != 0 {
		return comparison
	}
	if left == nil {
		return 0
	}
	if comparison := compareStrings(left.Context, right.Context); comparison != 0 {
		return comparison
	}
	return compareStrings(left.Reference, right.Reference)
}

func compareUnsupportedCapability(left, right *contract.UnsupportedCapabilityDetail) int {
	if comparison := comparePresence(left != nil, right != nil); comparison != 0 {
		return comparison
	}
	if left == nil {
		return 0
	}
	if comparison := compareStrings(left.Capability, right.Capability); comparison != 0 {
		return comparison
	}
	return compareStrings(left.Detail, right.Detail)
}

func comparePresence(left, right bool) int {
	switch {
	case left == right:
		return 0
	case !left:
		return -1
	default:
		return 1
	}
}

func compareStrings(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func writeWebhooks(output *bytes.Buffer, webhooks []contract.WebhookEvaluation) {
	if webhooks == nil {
		output.WriteString("webhooks: null\n")
		return
	}
	if len(webhooks) == 0 {
		output.WriteString("webhooks: []\n")
		return
	}

	output.WriteString("webhooks:\n")
	for _, webhook := range webhooks {
		fmt.Fprintf(output, "  - index: %d\n", webhook.WebhookIndex)
		fmt.Fprintf(output, "    name: %s\n", strconv.Quote(webhook.WebhookName))
		fmt.Fprintf(output, "    sourcePath: %s\n", strconv.Quote(webhook.SourcePath))
		fmt.Fprintf(output, "    configurationKind: %s\n", webhook.ConfigurationKind)
		fmt.Fprintf(output, "    determination: %s\n", webhook.Determination)
		if webhook.Outcome == nil {
			output.WriteString("    outcome: null\n")
		} else {
			fmt.Fprintf(output, "    outcome: %s\n", *webhook.Outcome)
		}
		writeTerminalReasons(output, webhook.Trace)
		writeTrace(output, webhook.Trace)
		writeDiagnostics(output, "diagnostics", webhook.Diagnostics, 4)
	}
}

func writeTerminalReasons(output *bytes.Buffer, trace []contract.TraceStep) {
	reasons := make([]contract.ReasonCode, 0, 1)
	for _, step := range trace {
		if step.Terminal {
			reasons = append(reasons, step.ReasonCode)
		}
	}
	if len(reasons) == 0 {
		output.WriteString("    terminalReasons: []\n")
		return
	}
	output.WriteString("    terminalReasons:\n")
	for _, reason := range reasons {
		fmt.Fprintf(output, "      - %s\n", reason)
	}
}

func writeTrace(output *bytes.Buffer, trace []contract.TraceStep) {
	if trace == nil {
		output.WriteString("    trace: null\n")
		return
	}
	if len(trace) == 0 {
		output.WriteString("    trace: []\n")
		return
	}

	output.WriteString("    trace:\n")
	for _, step := range trace {
		fmt.Fprintf(output, "      - sequence: %d\n", step.Sequence)
		fmt.Fprintf(output, "        stage: %s\n", strconv.Quote(step.Stage))
		fmt.Fprintf(output, "        sourcePath: %s\n", strconv.Quote(step.SourcePath))
		fmt.Fprintf(output, "        result: %s\n", step.Result)
		fmt.Fprintf(output, "        reasonCode: %s\n", step.ReasonCode)
		fmt.Fprintf(output, "        terminal: %t\n", step.Terminal)
		fmt.Fprintf(output, "        pending: %t\n", step.Pending)
		fmt.Fprintf(output, "        discarded: %t\n", step.Discarded)
		writeInputSummary(output, step.InputSummary)
	}
}

func writeInputSummary(output *bytes.Buffer, summary contract.InputSummary) {
	if summary == nil {
		output.WriteString("        inputSummary: null\n")
		return
	}
	if len(summary) == 0 {
		output.WriteString("        inputSummary: {}\n")
		return
	}

	keys := make([]string, 0, len(summary))
	for key := range summary {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	output.WriteString("        inputSummary:\n")
	for _, key := range keys {
		fmt.Fprintf(output, "          %s: %s\n", strconv.Quote(key), strconv.Quote(summary[key]))
	}
}

func writeDiagnostics(output *bytes.Buffer, label string, diagnostics []contract.Diagnostic, indent int) {
	prefix := string(bytes.Repeat([]byte(" "), indent))
	if diagnostics == nil {
		fmt.Fprintf(output, "%s%s: null\n", prefix, label)
		return
	}
	if len(diagnostics) == 0 {
		fmt.Fprintf(output, "%s%s: []\n", prefix, label)
		return
	}

	fmt.Fprintf(output, "%s%s:\n", prefix, label)
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(output, "%s  - sourcePath: %s\n", prefix, strconv.Quote(diagnostic.SourcePath))
		fmt.Fprintf(output, "%s    code: %s\n", prefix, diagnostic.Code)
		fmt.Fprintf(output, "%s    severity: %s\n", prefix, diagnostic.Severity)
		fmt.Fprintf(output, "%s    message: %s\n", prefix, strconv.Quote(diagnostic.Message))
		if diagnostic.MissingContext != nil {
			fmt.Fprintf(output, "%s    missingContext: %s", prefix, strconv.Quote(diagnostic.MissingContext.Context))
			if diagnostic.MissingContext.Reference != "" {
				fmt.Fprintf(output, " (%s)", strconv.Quote(diagnostic.MissingContext.Reference))
			}
			output.WriteByte('\n')
		}
		if diagnostic.UnsupportedCapability != nil {
			fmt.Fprintf(output, "%s    unsupportedCapability: %s", prefix, strconv.Quote(diagnostic.UnsupportedCapability.Capability))
			if diagnostic.UnsupportedCapability.Detail != "" {
				fmt.Fprintf(output, " (%s)", strconv.Quote(diagnostic.UnsupportedCapability.Detail))
			}
			output.WriteByte('\n')
		}
	}
}
