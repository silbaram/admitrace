package contract_test

import (
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
)

func TestIsSupportedScenarioVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		apiVersion string
		kind       string
		want       bool
	}{
		{
			name:       "supported",
			apiVersion: contract.ScenarioAPIVersion,
			kind:       contract.ScenarioKind,
			want:       true,
		},
		{
			name:       "unknown API version",
			apiVersion: "admitrace.io/v1alpha2",
			kind:       contract.ScenarioKind,
			want:       false,
		},
		{
			name:       "unknown kind",
			apiVersion: contract.ScenarioAPIVersion,
			kind:       "ScenarioList",
			want:       false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := contract.IsSupportedScenarioVersion(test.apiVersion, test.kind)
			if got != test.want {
				t.Errorf("IsSupportedScenarioVersion(%q, %q) = %t, want %t", test.apiVersion, test.kind, got, test.want)
			}
		})
	}
}

func TestIsSupportedResultVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		schemaVersion string
		want          bool
	}{
		{name: "supported", schemaVersion: contract.ResultSchemaVersion, want: true},
		{name: "unknown", schemaVersion: "admitrace.result/v1alpha2", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := contract.IsSupportedResultVersion(test.schemaVersion)
			if got != test.want {
				t.Errorf("IsSupportedResultVersion(%q) = %t, want %t", test.schemaVersion, got, test.want)
			}
		})
	}
}

func TestIsSupportedCompatibilityProfile(t *testing.T) {
	t.Parallel()

	supported := contract.Kubernetes136DefaultProfile()
	tests := []struct {
		name    string
		profile contract.CompatibilityProfile
		want    bool
	}{
		{name: "supported exact profile", profile: supported, want: true},
		{
			name: "unknown profile ID",
			profile: contract.CompatibilityProfile{
				ID:                "kubernetes-1.36.2-custom",
				KubernetesVersion: supported.KubernetesVersion,
				FeatureGatePolicy: supported.FeatureGatePolicy,
			},
			want: false,
		},
		{
			name: "other patch version",
			profile: contract.CompatibilityProfile{
				ID:                supported.ID,
				KubernetesVersion: "1.36.3",
				FeatureGatePolicy: supported.FeatureGatePolicy,
			},
			want: false,
		},
		{
			name: "other minor version",
			profile: contract.CompatibilityProfile{
				ID:                supported.ID,
				KubernetesVersion: "1.35.2",
				FeatureGatePolicy: supported.FeatureGatePolicy,
			},
			want: false,
		},
		{
			name: "custom feature gates",
			profile: contract.CompatibilityProfile{
				ID:                supported.ID,
				KubernetesVersion: supported.KubernetesVersion,
				FeatureGatePolicy: "custom",
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := contract.IsSupportedCompatibilityProfile(test.profile)
			if got != test.want {
				t.Errorf("IsSupportedCompatibilityProfile(%+v) = %t, want %t", test.profile, got, test.want)
			}
		})
	}
}
