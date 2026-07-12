package scenario

import (
	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
)

// ApplyDefaults applies the explicit Kubernetes 1.36.2 routing defaults.
// It intentionally leaves unrelated Kubernetes API fields unchanged.
func ApplyDefaults(input *contract.Scenario) {
	if input == nil {
		return
	}
	kube136.ApplyValidatingRoutingDefaults(input.Configuration.Validating)
	kube136.ApplyMutatingRoutingDefaults(input.Configuration.Mutating)
}
