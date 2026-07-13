package scenario

import (
	"fmt"
	"sort"
	"strings"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	validation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Validate checks the supported Scenario identity and routing invariants without mutating input.
func Validate(input *contract.Scenario) error {
	if input == nil {
		return invalidInput(".", fmt.Errorf("scenario is required"))
	}
	if input.APIVersion != contract.ScenarioAPIVersion {
		return invalidValue(field.NewPath("apiVersion"), input.APIVersion, contract.ScenarioAPIVersion)
	}
	if input.Kind != contract.ScenarioKind {
		return invalidValue(field.NewPath("kind"), input.Kind, contract.ScenarioKind)
	}
	if strings.TrimSpace(input.Metadata.Name) == "" {
		return invalidInput(".metadata.name", fmt.Errorf("scenario name is required"))
	}
	if err := validateCompatibilityProfile(input.CompatibilityProfile); err != nil {
		return err
	}

	configurationKind, ok := input.Configuration.Kind()
	if !ok {
		return invalidInput(".configuration", fmt.Errorf("must contain exactly one of validatingWebhookConfiguration or mutatingWebhookConfiguration"))
	}

	var err error
	switch configurationKind {
	case contract.ConfigurationKindValidating:
		err = validateValidatingConfiguration(input.Configuration.Validating)
	case contract.ConfigurationKindMutating:
		err = validateMutatingConfiguration(input.Configuration.Mutating)
	}
	if err != nil {
		return err
	}
	if err := validateRequestRouting(input); err != nil {
		return err
	}
	return validateExpectations(input)
}

func validateExpectations(input *contract.Scenario) error {
	configured := configuredWebhookNames(input.Configuration)
	seen := make(map[string]int, len(input.Expectations))
	for i, expectation := range input.Expectations {
		path := field.NewPath("expectations").Index(i)
		if strings.TrimSpace(expectation.WebhookName) == "" {
			return invalidInput(path.Child("webhookName").String(), fmt.Errorf("webhook name is required"))
		}
		if firstIndex, duplicate := seen[expectation.WebhookName]; duplicate {
			return invalidInput(path.Child("webhookName").String(), fmt.Errorf("duplicate expectation for webhook %q first declared at index %d", expectation.WebhookName, firstIndex))
		}
		seen[expectation.WebhookName] = i
		if _, ok := configured[expectation.WebhookName]; !ok {
			return invalidInput(path.Child("webhookName").String(), fmt.Errorf("unknown configured webhook %q", expectation.WebhookName))
		}
		if err := expectation.Validate(); err != nil {
			return invalidInput(path.String(), err)
		}
	}
	return nil
}

func configuredWebhookNames(configuration contract.WebhookConfiguration) map[string]struct{} {
	if configuration.Validating != nil {
		names := make(map[string]struct{}, len(configuration.Validating.Webhooks))
		for _, webhook := range configuration.Validating.Webhooks {
			names[webhook.Name] = struct{}{}
		}
		return names
	}

	names := make(map[string]struct{}, len(configuration.Mutating.Webhooks))
	for _, webhook := range configuration.Mutating.Webhooks {
		names[webhook.Name] = struct{}{}
	}
	return names
}

func validateCompatibilityProfile(profile contract.CompatibilityProfile) error {
	want := contract.Kubernetes136DefaultProfile()
	if profile.ID != want.ID {
		return invalidValue(field.NewPath("compatibilityProfile", "id"), profile.ID, want.ID)
	}
	if profile.KubernetesVersion != want.KubernetesVersion {
		return invalidValue(field.NewPath("compatibilityProfile", "kubernetesVersion"), profile.KubernetesVersion, want.KubernetesVersion)
	}
	if profile.FeatureGatePolicy != want.FeatureGatePolicy {
		return invalidValue(field.NewPath("compatibilityProfile", "featureGatePolicy"), profile.FeatureGatePolicy, want.FeatureGatePolicy)
	}
	return nil
}

func validateValidatingConfiguration(configuration *kube136.ValidatingWebhookConfiguration) error {
	basePath := field.NewPath("configuration", "validatingWebhookConfiguration")
	if err := validateConfigurationTypeMeta(configuration.APIVersion, configuration.Kind, string(contract.ConfigurationKindValidating), basePath); err != nil {
		return err
	}

	seenNames := make(map[string]int, len(configuration.Webhooks))
	for i := range configuration.Webhooks {
		webhook := &configuration.Webhooks[i]
		webhookPath := basePath.Child("webhooks").Index(i)
		if err := validateWebhookName(webhook.Name, seenNames, i, webhookPath.Child("name")); err != nil {
			return err
		}
		if err := validateFailurePolicy(webhook.FailurePolicy, webhookPath.Child("failurePolicy")); err != nil {
			return err
		}
		if err := validateMatchPolicy(webhook.MatchPolicy, webhookPath.Child("matchPolicy")); err != nil {
			return err
		}
		if err := validateLabelSelector(webhook.NamespaceSelector, webhookPath.Child("namespaceSelector")); err != nil {
			return err
		}
		if err := validateLabelSelector(webhook.ObjectSelector, webhookPath.Child("objectSelector")); err != nil {
			return err
		}
		if err := validateMatchConditions(webhook.MatchConditions, webhookPath.Child("matchConditions")); err != nil {
			return err
		}
		if err := validateRules(webhook.Rules, webhookPath.Child("rules")); err != nil {
			return err
		}
	}
	return nil
}

func validateMutatingConfiguration(configuration *kube136.MutatingWebhookConfiguration) error {
	basePath := field.NewPath("configuration", "mutatingWebhookConfiguration")
	if err := validateConfigurationTypeMeta(configuration.APIVersion, configuration.Kind, string(contract.ConfigurationKindMutating), basePath); err != nil {
		return err
	}

	seenNames := make(map[string]int, len(configuration.Webhooks))
	for i := range configuration.Webhooks {
		webhook := &configuration.Webhooks[i]
		webhookPath := basePath.Child("webhooks").Index(i)
		if err := validateWebhookName(webhook.Name, seenNames, i, webhookPath.Child("name")); err != nil {
			return err
		}
		if err := validateFailurePolicy(webhook.FailurePolicy, webhookPath.Child("failurePolicy")); err != nil {
			return err
		}
		if err := validateMatchPolicy(webhook.MatchPolicy, webhookPath.Child("matchPolicy")); err != nil {
			return err
		}
		if err := validateLabelSelector(webhook.NamespaceSelector, webhookPath.Child("namespaceSelector")); err != nil {
			return err
		}
		if err := validateLabelSelector(webhook.ObjectSelector, webhookPath.Child("objectSelector")); err != nil {
			return err
		}
		if err := validateMatchConditions(webhook.MatchConditions, webhookPath.Child("matchConditions")); err != nil {
			return err
		}
		if err := validateRules(webhook.Rules, webhookPath.Child("rules")); err != nil {
			return err
		}
	}
	return nil
}

func validateConfigurationTypeMeta(apiVersion, kind, wantKind string, path *field.Path) error {
	if apiVersion != kube136.AdmissionRegistrationAPIVersion {
		return invalidValue(path.Child("apiVersion"), apiVersion, kube136.AdmissionRegistrationAPIVersion)
	}
	if kind != wantKind {
		return invalidValue(path.Child("kind"), kind, wantKind)
	}
	return nil
}

func validateWebhookName(name string, seen map[string]int, index int, path *field.Path) error {
	// IsFullyQualifiedName is the validator used by Kubernetes webhook
	// configuration validation for its stronger fully-qualified DNS contract.
	if errs := validation.IsFullyQualifiedName(path, name); len(errs) > 0 {
		return invalidFieldErrors(errs)
	}
	if firstIndex, duplicate := seen[name]; duplicate {
		return invalidInput(path.String(), fmt.Errorf("duplicate webhook name %q first declared at index %d", name, firstIndex))
	}
	seen[name] = index
	return nil
}

func validateFailurePolicy(policy *admissionregistrationv1.FailurePolicyType, path *field.Path) error {
	if policy == nil {
		return nil
	}
	if *policy != admissionregistrationv1.Fail && *policy != admissionregistrationv1.Ignore {
		return invalidInput(path.String(), fmt.Errorf("unsupported failurePolicy %q: want Fail or Ignore", *policy))
	}
	return nil
}

func validateMatchPolicy(policy *admissionregistrationv1.MatchPolicyType, path *field.Path) error {
	if policy == nil {
		return nil
	}
	if *policy != admissionregistrationv1.Exact && *policy != admissionregistrationv1.Equivalent {
		return invalidInput(path.String(), fmt.Errorf("unsupported matchPolicy %q: want Exact or Equivalent", *policy))
	}
	return nil
}

func validateLabelSelector(selector *metav1.LabelSelector, path *field.Path) error {
	errs := metav1validation.ValidateLabelSelector(
		selector,
		metav1validation.LabelSelectorValidationOptions{},
		path,
	)
	return invalidFieldErrors(errs)
}

func validateMatchConditions(conditions []admissionregistrationv1.MatchCondition, path *field.Path) error {
	if len(conditions) > kube136.MaximumWebhookMatchConditions {
		return invalidInput(path.String(), fmt.Errorf("contains %d match conditions: maximum is %d", len(conditions), kube136.MaximumWebhookMatchConditions))
	}

	seenNames := make(map[string]int, len(conditions))
	for i := range conditions {
		condition := &conditions[i]
		conditionPath := path.Index(i)
		namePath := conditionPath.Child("name")
		if condition.Name == "" {
			return invalidInput(namePath.String(), fmt.Errorf("match condition name is required"))
		}
		if messages := validation.IsQualifiedName(condition.Name); len(messages) > 0 {
			return invalidInput(namePath.String(), field.Invalid(namePath, condition.Name, strings.Join(messages, ",")))
		}
		if firstIndex, duplicate := seenNames[condition.Name]; duplicate {
			return invalidInput(namePath.String(), fmt.Errorf("duplicate match condition name %q first declared at index %d", condition.Name, firstIndex))
		}
		seenNames[condition.Name] = i
		if strings.TrimSpace(condition.Expression) == "" {
			return invalidInput(conditionPath.Child("expression").String(), fmt.Errorf("match condition expression is required"))
		}
	}
	return nil
}

func validateRules(rules []admissionregistrationv1.RuleWithOperations, path *field.Path) error {
	for ruleIndex := range rules {
		rule := &rules[ruleIndex]
		rulePath := path.Index(ruleIndex)
		if rule.Scope != nil && *rule.Scope != admissionregistrationv1.ClusterScope && *rule.Scope != admissionregistrationv1.NamespacedScope && *rule.Scope != admissionregistrationv1.AllScopes {
			return invalidInput(rulePath.Child("scope").String(), fmt.Errorf("unsupported rule scope %q: want Cluster, Namespaced, or *", *rule.Scope))
		}
		for operationIndex, operation := range rule.Operations {
			if !isSupportedRuleOperation(operation) {
				return invalidInput(rulePath.Child("operations").Index(operationIndex).String(), fmt.Errorf("unsupported operation %q", operation))
			}
		}
	}
	return nil
}

func validateRequestRouting(input *contract.Scenario) error {
	operation := input.Request.Operation
	if operation != admissionv1.Create && operation != admissionv1.Update && operation != admissionv1.Delete && operation != admissionv1.Connect {
		return invalidInput(".request.operation", fmt.Errorf("unsupported operation %q", operation))
	}
	if input.Request.Scope != contract.RequestScopeCluster && input.Request.Scope != contract.RequestScopeNamespaced {
		return invalidInput(".request.scope", fmt.Errorf("unsupported request scope %q: want Cluster or Namespaced", input.Request.Scope))
	}
	return nil
}

func isSupportedRuleOperation(operation admissionregistrationv1.OperationType) bool {
	switch operation {
	case admissionregistrationv1.OperationAll, admissionregistrationv1.Create, admissionregistrationv1.Update, admissionregistrationv1.Delete, admissionregistrationv1.Connect:
		return true
	default:
		return false
	}
}

func invalidValue(path *field.Path, got, want any) error {
	return invalidInput(path.String(), fmt.Errorf("unsupported value %q: want %q", got, want))
}

func invalidFieldErrors(errs field.ErrorList) error {
	if len(errs) == 0 {
		return nil
	}
	sort.SliceStable(errs, func(i, j int) bool {
		return errs[i].Field < errs[j].Field
	})
	return invalidInput(errs[0].Field, errs[0])
}
