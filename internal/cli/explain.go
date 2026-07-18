package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/silbaram/admitrace/internal/adapter"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	"github.com/silbaram/admitrace/internal/hydration"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/render"
	"github.com/silbaram/admitrace/internal/snapshot"
	"github.com/spf13/cobra"
	admissionv1 "k8s.io/api/admission/v1"
)

type explainOptions struct {
	file             string
	resource         string
	webhookConfig    string
	namespaceObject  string
	contextName      string
	kubeconfig       string
	operation        string
	user             string
	groups           []string
	userUID          string
	userExtra        []string
	snapshotOut      string
	resourceExplicit bool
}

type explainExecution struct {
	legacy    *contract.EvaluationResult
	resources []manifest.ManifestExplanation
}

func newExplainCommand(output *string, exitCode *ExitCode, dependencies commandDependencies) *cobra.Command {
	options := explainOptions{operation: string(admissionv1.Create)}
	command := &cobra.Command{
		Use:   "explain",
		Short: "Explain admission webhook routing decisions",
		Long: "Explain one legacy Scenario or raw Kubernetes resource manifests. " +
			"-f/--file is universal: one admitrace.io/v1alpha1 Scenario keeps legacy output, while other files, stdin, and directories use CREATE-only resource mode. Input errors identify the logical source and 1-based document index. " +
			"Offline resolution uses the generated 1.36.2 built-in catalog; CRDs require verified context discovery and are never guessed.\n\n" +
			"Resource mode is offline unless --context is explicit. Context hydration requires exact Kubernetes 1.36.2 and permits GET only: version, discovery, WebhookConfiguration LIST, and a needed Namespace GET. " +
			"Explicit --webhook-config and --namespace-object files take precedence over those cluster reads. Admission identity is never inferred from kubeconfig; use --user and related flags.\n\n" +
			"called means routing selected the webhook; no HTTP request is sent and no webhook response or allow/deny result is observed.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			options.resourceExplicit = command.Flags().Changed("resource")
			if err := validateExplainFlags(command, options); err != nil {
				return newCommandError(ExitInvalidInput, true, err)
			}
			format, err := parseOutputFormat(*output)
			if err != nil {
				return newCommandError(ExitInvalidInput, true, err)
			}
			execution, err := executeExplanation(command.Context(), command.InOrStdin(), options, dependencies)
			if err != nil {
				return explainCommandError(err)
			}
			content, err := renderExplainExecution(execution, format)
			if err != nil {
				return internalError("render explanation", err)
			}
			if err := writeOutput(command.OutOrStdout(), content); err != nil {
				return internalError("write explanation output", err)
			}
			*exitCode = executionExitCode(execution)
			return nil
		},
	}
	flags := command.Flags()
	flags.StringVarP(&options.file, "file", "f", "", "universal Scenario or resource file, resource directory, or - for stdin")
	flags.StringVar(&options.resource, "resource", "", "explicit CREATE-only resource-mode file, directory, or - for stdin")
	flags.StringVar(&options.webhookConfig, "webhook-config", "", "WebhookConfiguration file or directory; takes precedence over cluster LIST")
	flags.StringVar(&options.namespaceObject, "namespace-object", "", "core/v1 Namespace file; takes precedence over Namespace GET")
	flags.StringVar(&options.contextName, "context", "", "explicit kubeconfig context for exact 1.36.2 GET-only hydration")
	flags.StringVar(&options.kubeconfig, "kubeconfig", "", "kubeconfig path used only with --context")
	flags.StringVar(&options.operation, "operation", string(admissionv1.Create), "admission operation; resource mode supports CREATE only")
	flags.StringVar(&options.user, "user", "", "explicit admission request username; never inferred from kubeconfig")
	flags.StringSliceVar(&options.groups, "group", nil, "explicit admission request group; repeat as needed")
	flags.StringVar(&options.userUID, "user-uid", "", "explicit admission request user UID")
	flags.StringArrayVar(&options.userExtra, "user-extra", nil, manifest.UserExtraHelp)
	flags.StringVar(&options.snapshotOut, "snapshot-out", "", "empty directory for exact replay Scenario snapshots; "+snapshot.PolicyNotice)
	return command
}

func validateExplainFlags(command *cobra.Command, options explainOptions) error {
	fileSet := command.Flags().Changed("file")
	resourceSet := command.Flags().Changed("resource")
	if fileSet && resourceSet {
		return errors.New("--file and --resource cannot be used together")
	}
	if !fileSet && !resourceSet {
		return errors.New("required flag \"file\" not set; use --file or --resource")
	}
	if fileSet && strings.TrimSpace(options.file) == "" {
		return errors.New("--file must not be empty")
	}
	if resourceSet && strings.TrimSpace(options.resource) == "" {
		return errors.New("--resource must not be empty")
	}
	if command.Flags().Changed("kubeconfig") && options.contextName == "" {
		return errors.New("--kubeconfig requires --context")
	}
	if command.Flags().Changed("context") && options.contextName == "" {
		return errors.New("--context must not be empty")
	}
	return nil
}

func executeExplanation(ctx context.Context, stdin io.Reader, options explainOptions, dependencies commandDependencies) (explainExecution, error) {
	inputName := options.file
	if options.resourceExplicit {
		inputName = options.resource
	}
	decoded, err := decodeCommandInput(stdin, inputName)
	if err != nil {
		return explainExecution{}, fmt.Errorf("read Scenario file or resource input and decode Scenario: %w", err)
	}
	if decoded.Mode == manifest.InputModeLegacyScenario {
		if options.resourceExplicit {
			return explainExecution{}, &contract.InvalidInputError{Field: "resource", Err: errors.New("--resource requires a Kubernetes resource, not a Scenario")}
		}
		if hasResourceOnlyOptions(options) {
			return explainExecution{}, &contract.InvalidInputError{Field: "file", Err: errors.New("resource-mode flags cannot be used with a legacy Scenario")}
		}
		result, err := evaluateLegacyScenario(ctx, decoded.Scenario)
		if err != nil {
			return explainExecution{}, err
		}
		return explainExecution{legacy: &result}, nil
	}
	if options.webhookConfig == "" && options.contextName == "" {
		return explainExecution{}, newCommandError(ExitInvalidInput, true, &contract.InvalidInputError{Field: "webhook-config", Err: errors.New("resource mode requires --webhook-config or --context")})
	}
	if err := manifest.ValidateOperation(admissionv1.Operation(strings.ToUpper(options.operation))); err != nil {
		return explainExecution{}, err
	}
	resources, err := resourceDocuments(decoded.Documents)
	if err != nil {
		return explainExecution{}, err
	}
	configurations, err := decodeConfigurationInput(options.webhookConfig)
	if err != nil {
		return explainExecution{}, err
	}
	namespace, err := decodeNamespaceInput(options.namespaceObject)
	if err != nil {
		return explainExecution{}, err
	}
	identity, err := manifest.NewIdentity(manifest.IdentityOptions{
		User:      options.user,
		Groups:    options.groups,
		UID:       options.userUID,
		UserExtra: options.userExtra,
	})
	if err != nil {
		return explainExecution{}, err
	}
	selected, err := connectHydration(ctx, options, dependencies)
	if err != nil {
		if mismatch, ok := profileMismatchExplanations(resources, configurations, identity, err); ok {
			return explainExecution{resources: mismatch}, nil
		}
		return explainExecution{}, err
	}
	resolved, err := adapter.Resolve(ctx, resources, adapter.Options{
		FileConfigurations: configurations,
		FileNamespace:      namespace,
		Hydration:          selected,
		Identity:           identity,
	})
	if err != nil {
		return explainExecution{}, err
	}
	explanations, err := evaluateResolvedResources(ctx, resolved)
	if err != nil {
		return explainExecution{}, err
	}
	if options.snapshotOut != "" {
		if dependencies.writeSnapshots == nil {
			return explainExecution{}, &contract.InternalError{Operation: "write Scenario snapshots", Err: errors.New("snapshot writer is required")}
		}
		if err := dependencies.writeSnapshots(options.snapshotOut, resolved.BuiltScenarios); err != nil {
			var snapshotError *snapshot.Error
			if !errors.As(err, &snapshotError) {
				return explainExecution{}, &contract.InternalError{Operation: "write Scenario snapshots", Err: err}
			}
			explanations = applySnapshotRefusal(explanations, snapshotError)
		}
	}
	return explainExecution{resources: explanations}, nil
}

func applySnapshotRefusal(explanations []manifest.ManifestExplanation, snapshotError *snapshot.Error) []manifest.ManifestExplanation {
	result := append([]manifest.ManifestExplanation(nil), explanations...)
	for index := range result {
		if snapshotError.Resource.Label != "" && snapshotError.Resource != result[index].Resource {
			continue
		}
		diagnostic := snapshotError.Diagnostic()
		if diagnostic.SourceLabel == "" {
			diagnostic.SourceLabel = result[index].Resource.Label
			diagnostic.DocumentIndex = result[index].Resource.DocumentIndex
		}
		result[index].Diagnostics = append(result[index].Diagnostics, diagnostic)
	}
	return result
}

func profileMismatchExplanations(
	resources []manifest.Document,
	configurations []manifest.ConfigurationInput,
	identity manifest.Identity,
	err error,
) ([]manifest.ManifestExplanation, bool) {
	var hydrationError *hydration.Error
	if !errors.As(err, &hydrationError) || hydrationError.Kind != hydration.ErrorKindProfileMismatch || hydrationError.ProfileMatch == nil {
		return nil, false
	}
	configurationStatus := manifest.ContextStatus{Status: manifest.CompletenessMissing}
	if len(configurations) > 0 {
		configurationStatus = manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: configurations[0].Source.Label}
	}
	identityStatus := manifest.ContextStatus{Status: manifest.CompletenessNotRequired}
	if identity.Provided() {
		identityStatus = manifest.ContextStatus{Status: manifest.CompletenessProvided, SourceLabel: "explicit-admission-user"}
	}
	result := make([]manifest.ManifestExplanation, len(resources))
	for index, resource := range resources {
		result[index] = manifest.ManifestExplanation{
			SchemaVersion: manifest.ExplanationSchemaVersion,
			Resource:      resource.Source,
			ProfileMatch:  *hydrationError.ProfileMatch,
			ContextCompleteness: manifest.ContextCompleteness{
				Configuration: configurationStatus,
				Discovery:     manifest.ContextStatus{Status: manifest.CompletenessUnsupported},
				Namespace:     manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
				Identity:      identityStatus,
				Equivalence:   manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
				Authorization: manifest.ContextStatus{Status: manifest.CompletenessNotRequired},
			},
			Diagnostics: []manifest.Diagnostic{{
				Code:          manifest.DiagnosticCodeProfileMismatch,
				Severity:      contract.DiagnosticSeverityWarning,
				Message:       "connected Kubernetes version does not match the exact supported 1.36.2 profile",
				SourceLabel:   resource.Source.Label,
				DocumentIndex: resource.Source.DocumentIndex,
			}},
			Evaluations: []manifest.ConfigurationEvaluation{},
		}
	}
	return result, true
}

func decodeCommandInput(stdin io.Reader, inputName string) (*manifest.DecodedInput, error) {
	if inputName == "-" {
		decoded, err := manifest.Decode(stdin, manifest.SourceKindStdin, "stdin")
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return decoded, nil
	}
	return manifest.DecodePath(inputName)
}

func hasResourceOnlyOptions(options explainOptions) bool {
	return options.webhookConfig != "" || options.namespaceObject != "" || options.contextName != "" || options.kubeconfig != "" ||
		strings.ToUpper(options.operation) != string(admissionv1.Create) || options.user != "" || len(options.groups) > 0 ||
		options.userUID != "" || len(options.userExtra) > 0 || options.snapshotOut != ""
}

func evaluateLegacyScenario(ctx context.Context, input *contract.Scenario) (contract.EvaluationResult, error) {
	if input == nil {
		return contract.EvaluationResult{}, &contract.InternalError{Operation: "decode legacy Scenario", Err: errors.New("decoded Scenario is nil")}
	}
	snapshot, err := evaluation.SnapshotFromScenario(*input)
	if err != nil {
		return contract.EvaluationResult{}, fmt.Errorf("prepare Scenario: %w", err)
	}
	return evaluation.NewEvaluator().Evaluate(ctx, snapshot), nil
}

func resourceDocuments(documents []manifest.Document) ([]manifest.Document, error) {
	resources := make([]manifest.Document, 0, len(documents))
	for _, document := range documents {
		if document.Class != manifest.DocumentClassResource {
			return nil, &manifest.DocumentError{Source: document.Source, Err: &contract.InvalidInputError{Field: ".kind", Err: errors.New("resource input must contain only Kubernetes resources")}}
		}
		resources = append(resources, document)
	}
	return resources, nil
}

func decodeConfigurationInput(path string) ([]manifest.ConfigurationInput, error) {
	if path == "" {
		return nil, nil
	}
	if path == "-" {
		return nil, &contract.InvalidInputError{Field: "webhook-config", Err: errors.New("stdin is reserved for the primary resource input")}
	}
	decoded, err := manifest.DecodePath(path)
	if err != nil {
		return nil, fmt.Errorf("decode --webhook-config: %w", err)
	}
	if decoded.Mode != manifest.InputModeResource {
		return nil, &contract.InvalidInputError{Field: "webhook-config", Err: errors.New("Scenario is not a WebhookConfiguration")}
	}
	result := make([]manifest.ConfigurationInput, 0, len(decoded.Documents))
	for _, document := range decoded.Documents {
		configuration, err := manifest.ConfigurationFromDocument(document)
		if err != nil {
			return nil, fmt.Errorf("decode --webhook-config: %w", err)
		}
		result = append(result, configuration)
	}
	return result, nil
}

func decodeNamespaceInput(path string) (*manifest.Document, error) {
	if path == "" {
		return nil, nil
	}
	if path == "-" {
		return nil, &contract.InvalidInputError{Field: "namespace-object", Err: errors.New("stdin is reserved for the primary resource input")}
	}
	decoded, err := manifest.DecodePath(path)
	if err != nil {
		return nil, fmt.Errorf("decode --namespace-object: %w", err)
	}
	if decoded.Mode != manifest.InputModeResource || len(decoded.Documents) != 1 || decoded.Documents[0].Class != manifest.DocumentClassNamespace {
		return nil, &contract.InvalidInputError{Field: "namespace-object", Err: errors.New("expected exactly one core/v1 Namespace document")}
	}
	return &decoded.Documents[0], nil
}

func connectHydration(ctx context.Context, options explainOptions, dependencies commandDependencies) (*adapter.Hydration, error) {
	if options.contextName == "" {
		return nil, nil
	}
	if dependencies.prepareHydration == nil {
		return nil, &contract.InternalError{Operation: "initialize hydration", Err: errors.New("hydration connector factory is required")}
	}
	return dependencies.prepareHydration(ctx, hydration.Options{Context: options.contextName, KubeconfigPath: options.kubeconfig})
}

func evaluateResolvedResources(ctx context.Context, resolved adapter.Result) ([]manifest.ManifestExplanation, error) {
	result := make([]manifest.ManifestExplanation, len(resolved.Resources))
	for index, resource := range resolved.Resources {
		evaluations := make([]manifest.AdapterEvaluation, 0)
		configurationEvaluations := make([]manifest.ConfigurationEvaluation, 0)
		diagnostics := append([]manifest.Diagnostic(nil), resource.Diagnostics...)
		for _, built := range resolved.BuiltScenarios {
			if built.Resource != resource.Resource {
				continue
			}
			evaluation, err := manifest.EvaluateBuiltScenario(ctx, built)
			if err != nil {
				return nil, err
			}
			evaluations = append(evaluations, evaluation)
			diagnostics = append(diagnostics, evaluation.Diagnostics...)
			configurationEvaluations = append(configurationEvaluations, manifest.ConfigurationEvaluation{
				Configuration: built.Configuration,
				Result:        evaluation.Result,
			})
		}
		explanation := manifest.ManifestExplanation{
			SchemaVersion:       manifest.ExplanationSchemaVersion,
			Resource:            resource.Resource,
			ProfileMatch:        resolved.ProfileMatch,
			ContextCompleteness: adapter.FinalizeCompleteness(resource.Completeness, evaluations),
			Diagnostics:         diagnostics,
			Evaluations:         configurationEvaluations,
		}
		if explanation.Diagnostics == nil {
			explanation.Diagnostics = []manifest.Diagnostic{}
		}
		if explanation.Evaluations == nil {
			explanation.Evaluations = []manifest.ConfigurationEvaluation{}
		}
		if err := explanation.Validate(); err != nil {
			return nil, &contract.InternalError{Operation: "validate resource explanation", Err: err}
		}
		result[index] = explanation
	}
	return result, nil
}

func renderExplainExecution(execution explainExecution, format outputFormat) ([]byte, error) {
	if execution.legacy != nil {
		return renderExplanation(*execution.legacy, format)
	}
	switch format {
	case outputText:
		return render.ManifestText(execution.resources)
	case outputJSON:
		return render.ManifestJSON(execution.resources)
	default:
		return nil, fmt.Errorf("unsupported output format %q", format)
	}
}

func renderExplanation(result contract.EvaluationResult, format outputFormat) ([]byte, error) {
	switch format {
	case outputText:
		return render.Text(result)
	case outputJSON:
		return render.JSON(result)
	default:
		return nil, fmt.Errorf("unsupported output format %q", format)
	}
}

func executionExitCode(execution explainExecution) ExitCode {
	if execution.legacy != nil {
		return explanationExitCode(*execution.legacy)
	}
	codes := make([]ExitCode, 0, len(execution.resources))
	for _, explanation := range execution.resources {
		codes = append(codes, manifestExplanationExitCode(explanation))
	}
	return SelectExitCode(codes...)
}

func manifestExplanationExitCode(explanation manifest.ManifestExplanation) ExitCode {
	for _, diagnostic := range explanation.Diagnostics {
		switch diagnostic.Code {
		case manifest.DiagnosticCodeIncomplete,
			manifest.DiagnosticCodeProfileMismatch,
			manifest.DiagnosticCodeIdentityContextMissing,
			manifest.DiagnosticCodeSnapshotRefused:
			return ExitIncompleteEvaluation
		}
	}
	for _, status := range []manifest.ContextStatus{
		explanation.ContextCompleteness.Configuration,
		explanation.ContextCompleteness.Discovery,
		explanation.ContextCompleteness.Namespace,
		explanation.ContextCompleteness.Identity,
		explanation.ContextCompleteness.Equivalence,
		explanation.ContextCompleteness.Authorization,
	} {
		if status.Status == manifest.CompletenessMissing || status.Status == manifest.CompletenessForbidden || status.Status == manifest.CompletenessUnsupported {
			return ExitIncompleteEvaluation
		}
	}
	for _, evaluation := range explanation.Evaluations {
		if code := explanationExitCode(evaluation.Result); code != ExitSuccess {
			return code
		}
	}
	return ExitSuccess
}

func explainCommandError(err error) error {
	var existing *commandError
	if errors.As(err, &existing) {
		return err
	}
	var operationError *manifest.OperationError
	if errors.As(err, &operationError) {
		return newCommandError(ExitInvalidInput, true, err)
	}
	if errors.Is(err, contract.ErrInvalidInput) || errors.Is(err, contract.ErrResourceLimit) {
		return newCommandError(ExitInvalidInput, false, err)
	}
	if errors.Is(err, contract.ErrUnsupportedCapability) || errors.Is(err, contract.ErrMissingContext) {
		return newCommandError(ExitIncompleteEvaluation, false, err)
	}
	var hydrationError *hydration.Error
	if errors.As(err, &hydrationError) {
		return newCommandError(ExitIncompleteEvaluation, false, err)
	}
	return internalError("explain input", err)
}

func explanationExitCode(result contract.EvaluationResult) ExitCode {
	codes := make([]ExitCode, 0, len(result.Webhooks)+1)
	codes = append(codes, diagnosticExitCode(result.Diagnostics))
	for _, webhook := range result.Webhooks {
		codes = append(codes, diagnosticExitCode(webhook.Diagnostics))
		if webhook.Determination != contract.DeterminationDeterminate {
			codes = append(codes, ExitIncompleteEvaluation)
		}
	}
	return SelectExitCode(codes...)
}

func diagnosticExitCode(diagnostics []contract.Diagnostic) ExitCode {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == contract.ReasonCodeInternalError {
			return ExitInternalError
		}
	}
	return ExitSuccess
}
