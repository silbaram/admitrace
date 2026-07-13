package beta_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	"github.com/silbaram/admitrace/internal/scenario"
)

type betaReport struct {
	SchemaVersion         string            `json:"schemaVersion"`
	CompatibilityProfile  string            `json:"compatibilityProfile"`
	Projects              []betaProject     `json:"projects"`
	Summary               betaSummary       `json:"summary"`
	TraceFeedback         []string          `json:"traceFeedback"`
	ScopeChangeCandidates []json.RawMessage `json:"scopeChangeCandidates"`
}

type betaProject struct {
	Project           string                 `json:"project"`
	Version           string                 `json:"version"`
	Commit            string                 `json:"commit"`
	License           string                 `json:"license"`
	SourceURL         string                 `json:"sourceURL"`
	SourceSHA256      string                 `json:"sourceSHA256"`
	LicenseURL        string                 `json:"licenseURL"`
	LicenseSHA256     string                 `json:"licenseSHA256"`
	SupportingSources []betaSupportingSource `json:"supportingSources,omitempty"`
	Scenario          string                 `json:"scenario"`
	ConfigurationKind string                 `json:"configurationKind"`
	Oracle            string                 `json:"oracle"`
	Observations      []betaObservation      `json:"observations"`
	Webhooks          int                    `json:"webhooks"`
	Determinate       int                    `json:"determinate"`
	Indeterminate     int                    `json:"indeterminate"`
	Unsupported       int                    `json:"unsupported"`
	Called            int                    `json:"called"`
	Skipped           int                    `json:"skipped"`
}

type betaSupportingSource struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type betaObservation struct {
	WebhookName        string `json:"webhookName"`
	Outcome            string `json:"outcome"`
	TerminalReasonCode string `json:"terminalReasonCode"`
}

type betaSummary struct {
	Projects           int     `json:"projects"`
	Scenarios          int     `json:"scenarios"`
	Webhooks           int     `json:"webhooks"`
	Determinate        int     `json:"determinate"`
	Indeterminate      int     `json:"indeterminate"`
	Unsupported        int     `json:"unsupported"`
	Incomplete         int     `json:"incomplete"`
	IncompleteRate     float64 `json:"incompleteRate"`
	Called             int     `json:"called"`
	Skipped            int     `json:"skipped"`
	SemanticMismatches int     `json:"semanticMismatches"`
}

func TestReportMatchesFreshScenarioEvaluation(t *testing.T) {
	report := readBetaReport(t)
	if got, want := report.SchemaVersion, "admitrace.beta-validation/v1alpha1"; got != want {
		t.Fatalf("schemaVersion = %q, want %q", got, want)
	}
	if got, want := report.CompatibilityProfile, "kubernetes-1.36.2-defaults"; got != want {
		t.Fatalf("compatibilityProfile = %q, want %q", got, want)
	}
	if got, want := len(report.Projects), 2; got != want {
		t.Fatalf("project count = %d, want %d", got, want)
	}
	if len(report.TraceFeedback) == 0 {
		t.Fatal("traceFeedback = empty, want recorded usability feedback")
	}
	if len(report.ScopeChangeCandidates) != 0 {
		t.Fatalf("scopeChangeCandidates = %d, want no silently accepted scope changes", len(report.ScopeChangeCandidates))
	}

	seenScenarios := make(map[string]struct{}, len(report.Projects))
	wantSummary := betaSummary{Projects: len(report.Projects), Scenarios: len(report.Projects)}
	for _, project := range report.Projects {
		t.Run(project.Project, func(t *testing.T) {
			validateProvenance(t, project)
			if _, duplicate := seenScenarios[project.Scenario]; duplicate {
				t.Fatalf("scenario = %q, want unique", project.Scenario)
			}
			seenScenarios[project.Scenario] = struct{}{}

			input, result := evaluateScenario(t, project.Scenario)
			if got, want := string(result.ConfigurationKind), project.ConfigurationKind; got != want {
				t.Errorf("configuration kind = %q, want %q", got, want)
			}
			if got, want := len(result.Webhooks), len(project.Observations); got != want {
				t.Fatalf("result Webhook count = %d, want %d", got, want)
			}
			if got, want := len(input.Expectations), len(result.Webhooks); got != want {
				t.Fatalf("expectation count = %d, want %d", got, want)
			}

			actual := betaProject{Webhooks: len(result.Webhooks)}
			for index, webhook := range result.Webhooks {
				observation := project.Observations[index]
				if got, want := webhook.WebhookName, observation.WebhookName; got != want {
					t.Errorf("Webhook[%d] name = %q, want %q", index, got, want)
				}
				expectation := input.Expectations[index]
				if got, want := expectation.WebhookName, webhook.WebhookName; got != want {
					t.Errorf("expectation[%d] Webhook = %q, want %q", index, got, want)
				}
				if got, want := webhook.Determination, expectation.Determination; got != want {
					t.Errorf("Webhook %q determination = %q, want %q", webhook.WebhookName, got, want)
				}
				terminal, ok := terminalReason(webhook.Trace)
				if !ok {
					t.Errorf("Webhook %q terminal reason = absent, want one", webhook.WebhookName)
				}
				if got, want := string(terminal), observation.TerminalReasonCode; got != want {
					t.Errorf("Webhook %q terminal reason = %q, want %q", webhook.WebhookName, got, want)
				}
				if got, want := terminal, expectation.TerminalReasonCode; got != want {
					t.Errorf("Webhook %q terminal reason = %q, expectation %q", webhook.WebhookName, got, want)
				}

				switch webhook.Determination {
				case contract.DeterminationDeterminate:
					actual.Determinate++
				case contract.DeterminationIndeterminate:
					actual.Indeterminate++
				case contract.DeterminationUnsupported:
					actual.Unsupported++
				}
				if webhook.Outcome == nil {
					if observation.Outcome != "" {
						t.Errorf("Webhook %q outcome = absent, report %q", webhook.WebhookName, observation.Outcome)
					}
					continue
				}
				if got, want := string(*webhook.Outcome), observation.Outcome; got != want {
					t.Errorf("Webhook %q outcome = %q, want %q", webhook.WebhookName, got, want)
				}
				if expectation.Outcome == nil || *expectation.Outcome != *webhook.Outcome {
					t.Errorf("Webhook %q outcome = %q, want matching expectation", webhook.WebhookName, *webhook.Outcome)
				}
				switch *webhook.Outcome {
				case contract.OutcomeCalled:
					actual.Called++
				case contract.OutcomeSkipped:
					actual.Skipped++
				}
			}

			compareProjectCounts(t, actual, project)
			wantSummary.Webhooks += actual.Webhooks
			wantSummary.Determinate += actual.Determinate
			wantSummary.Indeterminate += actual.Indeterminate
			wantSummary.Unsupported += actual.Unsupported
			wantSummary.Called += actual.Called
			wantSummary.Skipped += actual.Skipped
		})
	}

	wantSummary.Incomplete = wantSummary.Indeterminate + wantSummary.Unsupported
	if wantSummary.Webhooks > 0 {
		wantSummary.IncompleteRate = float64(wantSummary.Incomplete) / float64(wantSummary.Webhooks)
	}
	if got, want := report.Summary, wantSummary; got != want {
		t.Errorf("summary = %+v, want %+v", got, want)
	}
}

func readBetaReport(t *testing.T) betaReport {
	t.Helper()
	file, err := os.Open("report.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var report betaReport
	if err := decoder.Decode(&report); err != nil {
		t.Fatalf("decode report.json: %v", err)
	}
	return report
}

func validateProvenance(t *testing.T, project betaProject) {
	t.Helper()
	if project.Project == "" || project.Version == "" || project.Oracle == "" {
		t.Fatalf("provenance identity = %+v, want project, version, and oracle", project)
	}
	if project.License != "Apache-2.0" {
		t.Errorf("license = %q, want Apache-2.0", project.License)
	}
	for _, check := range []struct {
		label  string
		value  string
		length int
	}{
		{label: "commit", value: project.Commit, length: 40},
		{label: "sourceSHA256", value: project.SourceSHA256, length: 64},
		{label: "licenseSHA256", value: project.LicenseSHA256, length: 64},
	} {
		if !validHex(check.value, check.length) {
			t.Errorf("%s = %q, want lowercase hexadecimal digest", check.label, check.value)
		}
	}
	for _, check := range []struct {
		label string
		value string
	}{
		{label: "sourceURL", value: project.SourceURL},
		{label: "licenseURL", value: project.LicenseURL},
	} {
		if !strings.HasPrefix(check.value, "https://raw.githubusercontent.com/") {
			t.Errorf("%s = %q, want pinned raw GitHub URL", check.label, check.value)
		}
	}
	for _, source := range project.SupportingSources {
		if !strings.HasPrefix(source.URL, "https://raw.githubusercontent.com/") || !validHex(source.SHA256, 64) {
			t.Errorf("supporting source = %+v, want pinned URL and SHA-256", source)
		}
	}
}

func validHex(value string, length int) bool {
	if len(value) != length || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func evaluateScenario(t *testing.T, path string) (*contract.Scenario, contract.EvaluationResult) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Scenario %s: %v", path, err)
	}
	input, err := scenario.Decode(data)
	if err != nil {
		t.Fatalf("decode Scenario %s: %v", path, err)
	}
	snapshot, err := evaluation.SnapshotFromScenario(*input)
	if err != nil {
		t.Fatalf("build Scenario snapshot %s: %v", path, err)
	}
	result := evaluation.NewEvaluator().Evaluate(context.Background(), snapshot)
	if err := result.Validate(); err != nil {
		t.Fatalf("validate Scenario result %s: %v", path, err)
	}
	return input, result
}

func terminalReason(trace []contract.TraceStep) (contract.ReasonCode, bool) {
	var reason contract.ReasonCode
	found := false
	for _, step := range trace {
		if !step.Terminal {
			continue
		}
		if found {
			return "", false
		}
		reason = step.ReasonCode
		found = true
	}
	return reason, found
}

func compareProjectCounts(t *testing.T, actual, recorded betaProject) {
	t.Helper()
	for _, check := range []struct {
		label    string
		actual   int
		recorded int
	}{
		{label: "webhooks", actual: actual.Webhooks, recorded: recorded.Webhooks},
		{label: "determinate", actual: actual.Determinate, recorded: recorded.Determinate},
		{label: "indeterminate", actual: actual.Indeterminate, recorded: recorded.Indeterminate},
		{label: "unsupported", actual: actual.Unsupported, recorded: recorded.Unsupported},
		{label: "called", actual: actual.Called, recorded: recorded.Called},
		{label: "skipped", actual: actual.Skipped, recorded: recorded.Skipped},
	} {
		if check.actual != check.recorded {
			t.Errorf("%s = %d, report %d", check.label, check.actual, check.recorded)
		}
	}
}
