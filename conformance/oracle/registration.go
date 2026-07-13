package oracle

import (
	"context"
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigurationKind selects a validating or mutating Webhook configuration.
type ConfigurationKind string

const (
	// ValidatingConfiguration installs a ValidatingWebhookConfiguration.
	ValidatingConfiguration ConfigurationKind = "validating"
	// MutatingConfiguration installs a MutatingWebhookConfiguration.
	MutatingConfiguration ConfigurationKind = "mutating"
)

// Configuration describes one isolated Webhook registration.
type Configuration struct {
	Kind            ConfigurationKind
	Name            string
	WebhookName     string
	URL             string
	CABundle        []byte
	Group           string
	Version         string
	Resource        string
	MatchPolicy     admissionregistrationv1.MatchPolicyType
	MatchExpression string
}

// InstallConfiguration creates and tracks exactly one Webhook configuration.
func (h *Harness) InstallConfiguration(ctx context.Context, configuration Configuration) (func(context.Context) error, error) {
	if configuration.Name == "" || configuration.WebhookName == "" || configuration.URL == "" || len(configuration.CABundle) == 0 {
		return nil, &SetupError{Stage: SetupResource, Err: fmt.Errorf("configuration name, Webhook name, URL, and CA bundle are required")}
	}
	matchPolicy := configuration.MatchPolicy
	if matchPolicy == "" {
		matchPolicy = admissionregistrationv1.Exact
	}
	failurePolicy := admissionregistrationv1.Fail
	sideEffects := admissionregistrationv1.SideEffectClassNone
	clientConfig := admissionregistrationv1.WebhookClientConfig{
		URL:      &configuration.URL,
		CABundle: append([]byte(nil), configuration.CABundle...),
	}
	rules := []admissionregistrationv1.RuleWithOperations{{
		Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
		Rule: admissionregistrationv1.Rule{
			APIGroups:   []string{configuration.Group},
			APIVersions: []string{configuration.Version},
			Resources:   []string{configuration.Resource},
		},
	}}
	conditions := []admissionregistrationv1.MatchCondition(nil)
	if configuration.MatchExpression != "" {
		conditions = []admissionregistrationv1.MatchCondition{{
			Name:       "admitrace-oracle",
			Expression: configuration.MatchExpression,
		}}
	}

	var cleanup func(context.Context) error
	switch configuration.Kind {
	case ValidatingConfiguration:
		object := &admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: configuration.Name},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{{
				Name:                    configuration.WebhookName,
				ClientConfig:            clientConfig,
				Rules:                   rules,
				FailurePolicy:           &failurePolicy,
				MatchPolicy:             &matchPolicy,
				MatchConditions:         conditions,
				SideEffects:             &sideEffects,
				AdmissionReviewVersions: []string{"v1"},
			}},
		}
		if _, err := h.Kubernetes.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(ctx, object, metav1.CreateOptions{}); err != nil {
			return nil, &SetupError{Stage: SetupResource, Err: fmt.Errorf("create validating Webhook configuration %s: %w", configuration.Name, err)}
		}
		cleanup = func(ctx context.Context) error {
			err := h.Kubernetes.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(ctx, configuration.Name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete validating Webhook configuration %s: %w", configuration.Name, err)
			}
			return nil
		}
	case MutatingConfiguration:
		object := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: configuration.Name},
			Webhooks: []admissionregistrationv1.MutatingWebhook{{
				Name:                    configuration.WebhookName,
				ClientConfig:            clientConfig,
				Rules:                   rules,
				FailurePolicy:           &failurePolicy,
				MatchPolicy:             &matchPolicy,
				MatchConditions:         conditions,
				SideEffects:             &sideEffects,
				AdmissionReviewVersions: []string{"v1"},
			}},
		}
		if _, err := h.Kubernetes.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(ctx, object, metav1.CreateOptions{}); err != nil {
			return nil, &SetupError{Stage: SetupResource, Err: fmt.Errorf("create mutating Webhook configuration %s: %w", configuration.Name, err)}
		}
		cleanup = func(ctx context.Context) error {
			err := h.Kubernetes.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(ctx, configuration.Name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete mutating Webhook configuration %s: %w", configuration.Name, err)
			}
			return nil
		}
	default:
		return nil, &SetupError{Stage: SetupResource, Err: fmt.Errorf("unsupported configuration kind %q", configuration.Kind)}
	}
	h.track(cleanup)
	return cleanup, nil
}
