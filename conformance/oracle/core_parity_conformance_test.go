//go:build conformance

package oracle

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/silbaram/admitrace/internal/contract"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCoreRulesAndSelectorsParity(t *testing.T) {
	harness := startConformanceHarness(t)
	for _, testCase := range coreParityCases() {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := prepareCoreParityRequest(ctx, harness, testCase); err != nil {
				t.Fatal(err)
			}
			recorders := startCoreParityRecorders(t, ctx, len(testCase.webhooks))
			cleanupConfiguration, err := installCoreParityConfiguration(ctx, harness, testCase, recorders)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanupWithDeadline(t, "core parity Webhook configuration", cleanupConfiguration)

			product, err := evaluateCoreParityCase(ctx, testCase)
			if err != nil {
				t.Fatal(err)
			}
			requestErr := executeCoreParityRequest(ctx, harness, testCase)
			for index, recorder := range recorders {
				if err := recorder.Err(); err != nil {
					t.Fatal(err)
				}
				calls := recorder.Snapshot()
				lastReview := RecordedReview{}
				if len(calls) > 0 {
					lastReview = calls[len(calls)-1]
				}
				observation, err := Observe(0, len(calls), lastReview, requestErr)
				if err != nil {
					t.Fatalf("observe Webhook %q: %v", testCase.webhooks[index].name, err)
				}
				productResult := product.Webhooks[index]
				if productResult.Determination != contract.DeterminationDeterminate || productResult.Outcome == nil {
					t.Errorf("Webhook %q product result = determination %q outcome %s, want determinate outcome", productResult.WebhookName, productResult.Determination, outcomeText(productResult.Outcome))
					continue
				}
				if got, want := observation.Kind, observationForOutcome(*productResult.Outcome); got != want {
					t.Error(&coreParityMismatch{
						caseName: testCase.name, webhookName: testCase.webhooks[index].name,
						product: productResult, oracle: observation,
					})
				}
			}
		})
	}
}

func startCoreParityRecorders(t *testing.T, ctx context.Context, count int) []*Recorder {
	t.Helper()
	recorders := make([]*Recorder, count)
	for index := range recorders {
		recorder, err := StartRecorder(ctx)
		if err != nil {
			t.Fatal(err)
		}
		recorders[index] = recorder
		t.Cleanup(func() { closeRecorder(t, recorder) })
	}
	return recorders
}

func installCoreParityConfiguration(
	ctx context.Context,
	harness *Harness,
	testCase coreParityCase,
	recorders []*Recorder,
) (func(context.Context) error, error) {
	if len(recorders) != len(testCase.webhooks) {
		return nil, &ObservationContractError{Err: fmt.Errorf("recorder count %d does not match Webhook count %d", len(recorders), len(testCase.webhooks))}
	}
	failurePolicy := admissionregistrationv1.Fail
	matchPolicy := admissionregistrationv1.Exact
	sideEffects := admissionregistrationv1.SideEffectClassNone
	configurationName := "core-" + stableSuffix(testCase.name)

	switch testCase.kind {
	case ValidatingConfiguration:
		webhooks := make([]admissionregistrationv1.ValidatingWebhook, len(testCase.webhooks))
		for index, webhook := range testCase.webhooks {
			clientConfig, err := recorderClientConfig(recorders[index])
			if err != nil {
				return nil, err
			}
			webhooks[index] = admissionregistrationv1.ValidatingWebhook{
				Name: webhook.name, ClientConfig: clientConfig, Rules: webhook.rules,
				FailurePolicy: &failurePolicy, MatchPolicy: &matchPolicy,
				NamespaceSelector: webhook.namespaceSelector, ObjectSelector: webhook.objectSelector,
				SideEffects: &sideEffects, AdmissionReviewVersions: []string{"v1"},
			}
		}
		object := &admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: configurationName}, Webhooks: webhooks,
		}
		if _, err := harness.Kubernetes.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(ctx, object, metav1.CreateOptions{}); err != nil {
			return nil, &SetupError{Stage: SetupResource, Err: fmt.Errorf("create core parity validating Webhook configuration %s: %w", configurationName, err)}
		}
		cleanup := func(ctx context.Context) error {
			err := harness.Kubernetes.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(ctx, configurationName, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete core parity validating Webhook configuration %s: %w", configurationName, err)
			}
			return nil
		}
		harness.track(cleanup)
		return cleanup, nil
	case MutatingConfiguration:
		webhooks := make([]admissionregistrationv1.MutatingWebhook, len(testCase.webhooks))
		for index, webhook := range testCase.webhooks {
			clientConfig, err := recorderClientConfig(recorders[index])
			if err != nil {
				return nil, err
			}
			webhooks[index] = admissionregistrationv1.MutatingWebhook{
				Name: webhook.name, ClientConfig: clientConfig, Rules: webhook.rules,
				FailurePolicy: &failurePolicy, MatchPolicy: &matchPolicy,
				NamespaceSelector: webhook.namespaceSelector, ObjectSelector: webhook.objectSelector,
				SideEffects: &sideEffects, AdmissionReviewVersions: []string{"v1"},
			}
		}
		object := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: configurationName}, Webhooks: webhooks,
		}
		if _, err := harness.Kubernetes.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(ctx, object, metav1.CreateOptions{}); err != nil {
			return nil, &SetupError{Stage: SetupResource, Err: fmt.Errorf("create core parity mutating Webhook configuration %s: %w", configurationName, err)}
		}
		cleanup := func(ctx context.Context) error {
			err := harness.Kubernetes.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(ctx, configurationName, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete core parity mutating Webhook configuration %s: %w", configurationName, err)
			}
			return nil
		}
		harness.track(cleanup)
		return cleanup, nil
	default:
		return nil, &ObservationContractError{Err: fmt.Errorf("unsupported core parity configuration kind %q", testCase.kind)}
	}
}

func recorderClientConfig(recorder *Recorder) (admissionregistrationv1.WebhookClientConfig, error) {
	caBundle, err := recorder.CABundle()
	if err != nil {
		return admissionregistrationv1.WebhookClientConfig{}, err
	}
	url := recorder.URL()
	return admissionregistrationv1.WebhookClientConfig{URL: &url, CABundle: caBundle}, nil
}

func prepareCoreParityRequest(ctx context.Context, harness *Harness, testCase coreParityCase) error {
	request := testCase.request
	switch request.action {
	case createConfigMap, updateConfigMap, deleteConfigMap:
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: request.namespace, Labels: cloneLabels(testCase.namespaceLabels)}}
		if _, err := harness.Kubernetes.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{}); err != nil {
			return &SetupError{Stage: SetupResource, Err: fmt.Errorf("create core parity namespace %s: %w", request.namespace, err)}
		}
		trackNamespaceCleanup(harness, request.namespace)
		if request.action == createConfigMap {
			return nil
		}
		object := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: request.name, Namespace: request.namespace, Labels: cloneLabels(request.oldLabels),
		}}
		if _, err := harness.Kubernetes.CoreV1().ConfigMaps(request.namespace).Create(ctx, object, metav1.CreateOptions{}); err != nil {
			return &SetupError{Stage: SetupResource, Err: fmt.Errorf("create core parity ConfigMap %s/%s: %w", request.namespace, request.name, err)}
		}
		trackConfigMapCleanup(harness, request.namespace, request.name)
	case updateNamespaceStatus:
		object := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: request.name, Labels: cloneLabels(request.oldLabels)}}
		if _, err := harness.Kubernetes.CoreV1().Namespaces().Create(ctx, object, metav1.CreateOptions{}); err != nil {
			return &SetupError{Stage: SetupResource, Err: fmt.Errorf("create core parity Namespace %s: %w", request.name, err)}
		}
		trackNamespaceCleanup(harness, request.name)
	}
	return nil
}

func executeCoreParityRequest(ctx context.Context, harness *Harness, testCase coreParityCase) error {
	request := testCase.request
	switch request.action {
	case createConfigMap:
		_, err := harness.Kubernetes.CoreV1().ConfigMaps(request.namespace).Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: request.name, Namespace: request.namespace, Labels: cloneLabels(request.objectLabels)},
		}, metav1.CreateOptions{})
		if err == nil {
			trackConfigMapCleanup(harness, request.namespace, request.name)
		}
		return err
	case updateConfigMap:
		object, err := harness.Kubernetes.CoreV1().ConfigMaps(request.namespace).Get(ctx, request.name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		object.Labels = cloneLabels(request.objectLabels)
		_, err = harness.Kubernetes.CoreV1().ConfigMaps(request.namespace).Update(ctx, object, metav1.UpdateOptions{})
		return err
	case deleteConfigMap:
		return harness.Kubernetes.CoreV1().ConfigMaps(request.namespace).Delete(ctx, request.name, metav1.DeleteOptions{})
	case updateNamespaceStatus:
		object, err := harness.Kubernetes.CoreV1().Namespaces().Get(ctx, request.name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		object.Status.Phase = corev1.NamespaceActive
		_, err = harness.Kubernetes.CoreV1().Namespaces().UpdateStatus(ctx, object, metav1.UpdateOptions{})
		return err
	case createNamespace:
		_, err := harness.Kubernetes.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: request.name, Labels: cloneLabels(request.objectLabels)},
		}, metav1.CreateOptions{})
		if err == nil {
			trackNamespaceCleanup(harness, request.name)
		}
		return err
	case createClusterRole:
		_, err := harness.Kubernetes.RbacV1().ClusterRoles().Create(ctx, &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: request.name},
		}, metav1.CreateOptions{})
		if err == nil {
			harness.track(func(ctx context.Context) error {
				err := harness.Kubernetes.RbacV1().ClusterRoles().Delete(ctx, request.name, metav1.DeleteOptions{})
				if err != nil && !apierrors.IsNotFound(err) {
					return fmt.Errorf("delete core parity ClusterRole %s: %w", request.name, err)
				}
				return nil
			})
		}
		return err
	case createWebhookConfiguration:
		_, err := harness.Kubernetes.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(ctx, &admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: request.name},
		}, metav1.CreateOptions{})
		if err == nil {
			harness.track(func(ctx context.Context) error {
				err := harness.Kubernetes.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(ctx, request.name, metav1.DeleteOptions{})
				if err != nil && !apierrors.IsNotFound(err) {
					return fmt.Errorf("delete core parity target Webhook configuration %s: %w", request.name, err)
				}
				return nil
			})
		}
		return err
	default:
		return &ObservationContractError{Err: fmt.Errorf("unsupported core parity action %q", request.action)}
	}
}

func trackNamespaceCleanup(harness *Harness, name string) {
	harness.track(func(ctx context.Context) error {
		err := harness.Kubernetes.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete core parity Namespace %s: %w", name, err)
		}
		return nil
	})
}

func trackConfigMapCleanup(harness *Harness, namespace, name string) {
	harness.track(func(ctx context.Context) error {
		err := harness.Kubernetes.CoreV1().ConfigMaps(namespace).Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete core parity ConfigMap %s/%s: %w", namespace, name, err)
		}
		return nil
	})
}

func observationForOutcome(outcome contract.Outcome) ObservationKind {
	switch outcome {
	case contract.OutcomeCalled:
		return ObservationCalled
	case contract.OutcomeSkipped:
		return ObservationSkipped
	case contract.OutcomeRejectedBeforeCall:
		return ObservationRejectedBeforeCall
	default:
		return ""
	}
}
