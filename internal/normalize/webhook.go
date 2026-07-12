package normalize

import (
	"errors"
	"fmt"

	"github.com/silbaram/admitrace/internal/contract"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	validatingWebhookPath = ".configuration.validatingWebhookConfiguration.webhooks"
	mutatingWebhookPath   = ".configuration.mutatingWebhookConfiguration.webhooks"
)

// NormalizedWebhook contains the common routing inputs shared by validating
// and mutating admission webhooks.
type NormalizedWebhook struct {
	ConfigurationKind       contract.ConfigurationKind
	Name                    string
	InputIndex              int
	SourcePath              string
	Rules                   []Rule
	MatchPolicy             admissionregistrationv1.MatchPolicyType
	NamespaceSelector       *metav1.LabelSelector
	ObjectSelector          *metav1.LabelSelector
	MatchConditions         []admissionregistrationv1.MatchCondition
	FailurePolicy           admissionregistrationv1.FailurePolicyType
	SideEffects             *admissionregistrationv1.SideEffectClass
	AdmissionReviewVersions []string
}

// Rule is a pointer-free normalized admission routing rule.
type Rule struct {
	Operations  []admissionregistrationv1.OperationType
	APIGroups   []string
	APIVersions []string
	Resources   []string
	Scope       admissionregistrationv1.ScopeType
}

// Webhooks normalizes exactly one supported webhook configuration while
// preserving configuration and list order.
func Webhooks(configuration contract.WebhookConfiguration) ([]NormalizedWebhook, error) {
	kind, ok := configuration.Kind()
	if !ok {
		return nil, invalidInput(".configuration", "must contain exactly one webhook configuration")
	}

	switch kind {
	case contract.ConfigurationKindValidating:
		webhooks := configuration.Validating.Webhooks
		result := makeNormalizedWebhooks(webhooks)
		for i := range webhooks {
			webhook := &webhooks[i]
			normalized, err := normalizeWebhook(kind, i, validatingWebhookPath, webhookInput{
				name:                    webhook.Name,
				rules:                   webhook.Rules,
				matchPolicy:             webhook.MatchPolicy,
				namespaceSelector:       webhook.NamespaceSelector,
				objectSelector:          webhook.ObjectSelector,
				matchConditions:         webhook.MatchConditions,
				failurePolicy:           webhook.FailurePolicy,
				sideEffects:             webhook.SideEffects,
				admissionReviewVersions: webhook.AdmissionReviewVersions,
			})
			if err != nil {
				return nil, err
			}
			result[i] = normalized
		}
		return result, nil
	case contract.ConfigurationKindMutating:
		webhooks := configuration.Mutating.Webhooks
		result := makeNormalizedWebhooks(webhooks)
		for i := range webhooks {
			webhook := &webhooks[i]
			normalized, err := normalizeWebhook(kind, i, mutatingWebhookPath, webhookInput{
				name:                    webhook.Name,
				rules:                   webhook.Rules,
				matchPolicy:             webhook.MatchPolicy,
				namespaceSelector:       webhook.NamespaceSelector,
				objectSelector:          webhook.ObjectSelector,
				matchConditions:         webhook.MatchConditions,
				failurePolicy:           webhook.FailurePolicy,
				sideEffects:             webhook.SideEffects,
				admissionReviewVersions: webhook.AdmissionReviewVersions,
			})
			if err != nil {
				return nil, err
			}
			result[i] = normalized
		}
		return result, nil
	default:
		return nil, invalidInput(".configuration", "contains an unsupported webhook configuration")
	}
}

type webhookInput struct {
	name                    string
	rules                   []admissionregistrationv1.RuleWithOperations
	matchPolicy             *admissionregistrationv1.MatchPolicyType
	namespaceSelector       *metav1.LabelSelector
	objectSelector          *metav1.LabelSelector
	matchConditions         []admissionregistrationv1.MatchCondition
	failurePolicy           *admissionregistrationv1.FailurePolicyType
	sideEffects             *admissionregistrationv1.SideEffectClass
	admissionReviewVersions []string
}

func normalizeWebhook(
	kind contract.ConfigurationKind,
	index int,
	basePath string,
	input webhookInput,
) (NormalizedWebhook, error) {
	sourcePath := fmt.Sprintf("%s[%d]", basePath, index)
	if input.matchPolicy == nil {
		return NormalizedWebhook{}, invalidInput(sourcePath+".matchPolicy", "defaulted matchPolicy is required")
	}
	if input.failurePolicy == nil {
		return NormalizedWebhook{}, invalidInput(sourcePath+".failurePolicy", "defaulted failurePolicy is required")
	}

	rules := cloneRules(input.rules)
	for i := range rules {
		if input.rules[i].Scope == nil {
			path := fmt.Sprintf("%s.rules[%d].scope", sourcePath, i)
			return NormalizedWebhook{}, invalidInput(path, "defaulted rule scope is required")
		}
		rules[i].Scope = *input.rules[i].Scope
	}

	return NormalizedWebhook{
		ConfigurationKind:       kind,
		Name:                    input.name,
		InputIndex:              index,
		SourcePath:              sourcePath,
		Rules:                   rules,
		MatchPolicy:             *input.matchPolicy,
		NamespaceSelector:       cloneSelector(input.namespaceSelector),
		ObjectSelector:          cloneSelector(input.objectSelector),
		MatchConditions:         cloneSlice(input.matchConditions),
		FailurePolicy:           *input.failurePolicy,
		SideEffects:             cloneValue(input.sideEffects),
		AdmissionReviewVersions: cloneSlice(input.admissionReviewVersions),
	}, nil
}

func cloneValue[T any](input *T) *T {
	if input == nil {
		return nil
	}
	result := *input
	return &result
}

func makeNormalizedWebhooks[T any](input []T) []NormalizedWebhook {
	if input == nil {
		return nil
	}
	return make([]NormalizedWebhook, len(input))
}

func cloneRules(input []admissionregistrationv1.RuleWithOperations) []Rule {
	if input == nil {
		return nil
	}
	rules := make([]Rule, len(input))
	for i := range input {
		rules[i] = Rule{
			Operations:  cloneSlice(input[i].Operations),
			APIGroups:   cloneSlice(input[i].APIGroups),
			APIVersions: cloneSlice(input[i].APIVersions),
			Resources:   cloneSlice(input[i].Resources),
		}
	}
	return rules
}

func cloneSelector(input *metav1.LabelSelector) *metav1.LabelSelector {
	if input == nil {
		return nil
	}
	return input.DeepCopy()
}

func cloneSlice[T any](input []T) []T {
	if input == nil {
		return nil
	}
	result := make([]T, len(input))
	copy(result, input)
	return result
}

func invalidInput(field, message string) error {
	return &contract.InvalidInputError{Field: field, Err: errors.New(message)}
}
