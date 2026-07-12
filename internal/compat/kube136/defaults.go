package kube136

import admissionregistrationv1 "k8s.io/api/admissionregistration/v1"

const (
	// AdmissionRegistrationAPIVersion is the configuration API version supported by this profile.
	AdmissionRegistrationAPIVersion = "admissionregistration.k8s.io/v1"
	// MaximumWebhookMatchConditions is the Kubernetes 1.36 webhook match-condition limit.
	MaximumWebhookMatchConditions = 64
)

// ApplyValidatingRoutingDefaults applies only Kubernetes routing defaults used by admitrace.
func ApplyValidatingRoutingDefaults(configuration *ValidatingWebhookConfiguration) {
	if configuration == nil {
		return
	}
	for i := range configuration.Webhooks {
		webhook := &configuration.Webhooks[i]
		applyWebhookRoutingDefaults(&webhook.FailurePolicy, &webhook.MatchPolicy, webhook.Rules)
	}
}

// ApplyMutatingRoutingDefaults applies only Kubernetes routing defaults used by admitrace.
func ApplyMutatingRoutingDefaults(configuration *MutatingWebhookConfiguration) {
	if configuration == nil {
		return
	}
	for i := range configuration.Webhooks {
		webhook := &configuration.Webhooks[i]
		applyWebhookRoutingDefaults(&webhook.FailurePolicy, &webhook.MatchPolicy, webhook.Rules)
	}
}

func applyWebhookRoutingDefaults(
	failurePolicy **admissionregistrationv1.FailurePolicyType,
	matchPolicy **admissionregistrationv1.MatchPolicyType,
	rules []admissionregistrationv1.RuleWithOperations,
) {
	if *failurePolicy == nil {
		value := admissionregistrationv1.Fail
		*failurePolicy = &value
	}
	if *matchPolicy == nil {
		value := admissionregistrationv1.Equivalent
		*matchPolicy = &value
	}
	for i := range rules {
		if rules[i].Scope == nil {
			value := admissionregistrationv1.AllScopes
			rules[i].Scope = &value
		}
	}
}
