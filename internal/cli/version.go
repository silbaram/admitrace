package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/spf13/cobra"
)

const (
	goLanguageVersion  = "1.26.0"
	goToolchainVersion = "go1.26.5"
	envtestVersion     = "v0.24.1"
)

type outputFormat string

const (
	outputText outputFormat = "text"
	outputJSON outputFormat = "json"
)

// BuildMetadata identifies the AdmiTrace CLI build.
type BuildMetadata struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}

type versionInfo struct {
	CLI                  cliInfo                       `json:"cli"`
	Schemas              schemaBaseline                `json:"schemas"`
	CompatibilityProfile contract.CompatibilityProfile `json:"compatibilityProfile"`
	Dependencies         dependencyBaseline            `json:"dependencies"`
	Oracle               oracleBaseline                `json:"oracle"`
}

type cliInfo struct {
	Name string `json:"name"`
	BuildMetadata
}

type schemaBaseline struct {
	Scenario scenarioSchema `json:"scenario"`
	Result   resultSchema   `json:"result"`
}

type scenarioSchema struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

type resultSchema struct {
	SchemaVersion string `json:"schemaVersion"`
}

type dependencyBaseline struct {
	GoLanguage  string             `json:"goLanguage"`
	GoToolchain string             `json:"goToolchain"`
	Modules     []moduleDependency `json:"modules"`
}

type moduleDependency struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

type oracleBaseline struct {
	KubernetesVersion string `json:"kubernetesVersion"`
	EnvtestVersion    string `json:"envtestVersion"`
}

func newVersionCommand(output *string, build BuildMetadata) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show build and compatibility version information",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			format, err := parseOutputFormat(*output)
			if err != nil {
				return newCommandError(ExitInvalidInput, true, err)
			}
			content, err := renderVersion(newVersionInfo(build), format)
			if err != nil {
				return internalError("render version", err)
			}
			if err := writeOutput(command.OutOrStdout(), content); err != nil {
				return internalError("write version output", err)
			}
			return nil
		},
	}
}

func parseOutputFormat(value string) (outputFormat, error) {
	format := outputFormat(value)
	switch format {
	case outputText, outputJSON:
		return format, nil
	default:
		return "", fmt.Errorf("invalid output format %q: must be text or json", value)
	}
}

func newVersionInfo(build BuildMetadata) versionInfo {
	return versionInfo{
		CLI: cliInfo{
			Name:          "admitrace",
			BuildMetadata: build,
		},
		Schemas: schemaBaseline{
			Scenario: scenarioSchema{
				APIVersion: contract.ScenarioAPIVersion,
				Kind:       contract.ScenarioKind,
			},
			Result: resultSchema{SchemaVersion: contract.ResultSchemaVersion},
		},
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		Dependencies: dependencyBaseline{
			GoLanguage:  goLanguageVersion,
			GoToolchain: goToolchainVersion,
			Modules: []moduleDependency{
				{Path: "github.com/spf13/cobra", Version: "v1.10.2"},
				{Path: "k8s.io/api", Version: kube136.ModuleVersion},
				{Path: "k8s.io/apimachinery", Version: kube136.ModuleVersion},
				{Path: "k8s.io/apiserver", Version: kube136.ModuleVersion},
				{Path: "k8s.io/client-go", Version: kube136.ModuleVersion},
				{Path: "sigs.k8s.io/json", Version: "v0.0.0-20250730193827-2d320260d730"},
			},
		},
		Oracle: oracleBaseline{
			KubernetesVersion: kube136.KubernetesVersion,
			EnvtestVersion:    envtestVersion,
		},
	}
}

func renderVersion(info versionInfo, format outputFormat) ([]byte, error) {
	switch format {
	case outputJSON:
		content, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal JSON: %w", err)
		}
		return append(content, '\n'), nil
	case outputText:
		return []byte(renderVersionText(info)), nil
	default:
		return nil, fmt.Errorf("unsupported output format %q", format)
	}
}

func renderVersionText(info versionInfo) string {
	var text strings.Builder
	fmt.Fprintf(&text, "AdmiTrace %s\n", info.CLI.Version)
	fmt.Fprintf(&text, "commit: %s\n", info.CLI.Commit)
	fmt.Fprintf(&text, "build date: %s\n", info.CLI.BuildDate)
	fmt.Fprintf(&text, "Scenario schema: %s, kind %s\n", info.Schemas.Scenario.APIVersion, info.Schemas.Scenario.Kind)
	fmt.Fprintf(&text, "result schema: %s\n", info.Schemas.Result.SchemaVersion)
	fmt.Fprintf(&text, "compatibility profile: %s\n", info.CompatibilityProfile.ID)
	fmt.Fprintf(&text, "Kubernetes: %s (%s)\n", info.CompatibilityProfile.KubernetesVersion, info.CompatibilityProfile.FeatureGatePolicy)
	fmt.Fprintf(&text, "Go language: %s\n", info.Dependencies.GoLanguage)
	fmt.Fprintf(&text, "Go toolchain: %s\n", info.Dependencies.GoToolchain)
	for _, module := range info.Dependencies.Modules {
		fmt.Fprintf(&text, "dependency: %s %s\n", module.Path, module.Version)
	}
	fmt.Fprintf(&text, "conformance oracle: Kubernetes %s, envtest %s\n", info.Oracle.KubernetesVersion, info.Oracle.EnvtestVersion)
	return text.String()
}

func writeOutput(writer io.Writer, content []byte) error {
	written, err := writer.Write(content)
	if err != nil {
		return err
	}
	if written != len(content) {
		return io.ErrShortWrite
	}
	return nil
}
