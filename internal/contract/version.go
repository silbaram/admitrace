package contract

import "github.com/silbaram/admitrace/internal/compat/kube136"

const (
	// ScenarioAPIVersion is the only supported Scenario envelope version.
	ScenarioAPIVersion = "admitrace.io/v1alpha1"
	// ScenarioKind is the required kind for a Scenario envelope.
	ScenarioKind = "Scenario"
	// ResultSchemaVersion is the canonical result contract version.
	ResultSchemaVersion = "admitrace.result/v1alpha1"
	// Kubernetes136DefaultProfileID is the stable identity of the built-in Kubernetes profile.
	Kubernetes136DefaultProfileID = "kubernetes-1.36.2-defaults"
)

// FeatureGatePolicy identifies how Kubernetes feature gates are selected.
type FeatureGatePolicy string

const (
	// FeatureGatePolicyKubernetesDefaults selects the defaults of the exact Kubernetes release.
	FeatureGatePolicyKubernetesDefaults FeatureGatePolicy = "kubernetes-defaults"
)

// CompatibilityProfile pins the Kubernetes release and feature-gate policy used for evaluation.
type CompatibilityProfile struct {
	ID                string            `json:"id"`
	KubernetesVersion string            `json:"kubernetesVersion"`
	FeatureGatePolicy FeatureGatePolicy `json:"featureGatePolicy"`
}

// Kubernetes136DefaultProfile returns the sole compatibility profile supported by this release.
func Kubernetes136DefaultProfile() CompatibilityProfile {
	return CompatibilityProfile{
		ID:                Kubernetes136DefaultProfileID,
		KubernetesVersion: kube136.KubernetesVersion,
		FeatureGatePolicy: FeatureGatePolicyKubernetesDefaults,
	}
}

// IsSupportedScenarioVersion reports whether apiVersion and kind identify the supported Scenario envelope.
func IsSupportedScenarioVersion(apiVersion, kind string) bool {
	return apiVersion == ScenarioAPIVersion && kind == ScenarioKind
}

// IsSupportedResultVersion reports whether schemaVersion identifies the supported result contract.
func IsSupportedResultVersion(schemaVersion string) bool {
	return schemaVersion == ResultSchemaVersion
}

// IsSupportedCompatibilityProfile reports whether profile is the exact supported Kubernetes profile.
func IsSupportedCompatibilityProfile(profile CompatibilityProfile) bool {
	return profile == Kubernetes136DefaultProfile()
}
