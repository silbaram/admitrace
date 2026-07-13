//go:build conformance

package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	"github.com/silbaram/admitrace/internal/scenario"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBetaProjectParity(t *testing.T) {
	harness := startConformanceHarness(t)
	for _, testCase := range []struct {
		name string
		path string
	}{
		{name: "gatekeeper-v3.22.2-validating", path: betaScenarioPath("gatekeeper-v3.22.2-validating.yaml")},
		{name: "istio-1.30.0-mutating", path: betaScenarioPath("istio-1.30.0-mutating.yaml")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			input, product := loadBetaProductResult(t, ctx, testCase.path)
			prepareBetaNamespace(t, ctx, harness, input)
			recorders := startCoreParityRecorders(t, ctx, len(product.Webhooks))
			cleanupConfiguration, err := installBetaConfiguration(ctx, harness, input, recorders)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanupWithDeadline(t, "beta Webhook configuration", cleanupConfiguration)

			requestErr := executeBetaRequest(ctx, harness, input)
			for index, recorder := range recorders {
				if err := recorder.Err(); err != nil {
					t.Fatal(err)
				}
				allCalls := recorder.Snapshot()
				calls := betaTargetReviews(allCalls, input.Request)
				if unrelated := len(allCalls) - len(calls); unrelated > 0 {
					t.Logf("Webhook %q ignored %d unrelated API server call(s)", product.Webhooks[index].WebhookName, unrelated)
				}
				lastReview := RecordedReview{}
				if len(calls) > 0 {
					lastReview = calls[len(calls)-1]
				}
				observation, err := Observe(0, len(calls), lastReview, requestErr)
				if err != nil {
					t.Fatalf("observe Webhook %q: %v", product.Webhooks[index].WebhookName, err)
				}
				productResult := product.Webhooks[index]
				if productResult.Determination != contract.DeterminationDeterminate || productResult.Outcome == nil {
					t.Errorf("Webhook %q product result = determination %q outcome %v, want determinate outcome", productResult.WebhookName, productResult.Determination, productResult.Outcome)
					continue
				}
				wantObservation := observationForOutcome(*productResult.Outcome)
				if err := Compare(wantObservation, observation); err != nil {
					t.Errorf("Webhook %q: %v", productResult.WebhookName, err)
				}
				if wantObservation == ObservationCalled {
					if got, want := observation.CallCount, 1; got != want {
						t.Errorf("Webhook %q call count = %d, want %d", productResult.WebhookName, got, want)
					}
					verifyBetaAdmissionReview(t, productResult.WebhookName, input, observation.LastReview)
				} else if got := observation.CallCount; got != 0 {
					t.Errorf("Webhook %q call count = %d, want 0", productResult.WebhookName, got)
				}
			}
			if requestErr != nil {
				t.Errorf("submit beta API request: %v", requestErr)
			}
		})
	}
}

func TestBetaTargetReviews(t *testing.T) {
	request := contract.AdmissionRequest{
		Operation: admissionv1.Create,
		Resource:  metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		Namespace: "beta-gatekeeper",
		Name:      "beta-workload",
	}
	target := RecordedReview{
		UID: "target", Operation: request.Operation, Resource: request.Resource,
		Namespace: request.Namespace, Name: request.Name,
	}
	unrelated := RecordedReview{
		UID: "unrelated", Operation: admissionv1.Create,
		Resource:  metav1.GroupVersionResource{Version: "v1", Resource: "endpoints"},
		Namespace: "default", Name: "kubernetes",
	}

	got := betaTargetReviews([]RecordedReview{unrelated, target}, request)
	if len(got) != 1 || got[0] != target {
		t.Fatalf("betaTargetReviews() = %#v, want [%#v]", got, target)
	}
}

func betaTargetReviews(calls []RecordedReview, request contract.AdmissionRequest) []RecordedReview {
	result := make([]RecordedReview, 0, len(calls))
	for _, call := range calls {
		if call.Operation != request.Operation ||
			call.Resource != request.Resource ||
			call.Namespace != request.Namespace ||
			call.Name != request.Name {
			continue
		}
		result = append(result, call)
	}
	return result
}

func betaScenarioPath(name string) string {
	return filepath.Join("..", "..", "validation", "beta", "scenarios", name)
}

func loadBetaProductResult(t *testing.T, ctx context.Context, path string) (*contract.Scenario, contract.EvaluationResult) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read beta Scenario %s: %v", path, err)
	}
	input, err := scenario.Decode(data)
	if err != nil {
		t.Fatalf("decode beta Scenario %s: %v", path, err)
	}
	snapshot, err := evaluation.SnapshotFromScenario(*input)
	if err != nil {
		t.Fatalf("build beta snapshot %s: %v", path, err)
	}
	result := evaluation.NewEvaluator().Evaluate(ctx, snapshot)
	if err := result.Validate(); err != nil {
		t.Fatalf("validate beta result %s: %v", path, err)
	}
	compareBetaExpectations(t, input.Expectations, result.Webhooks)
	return input, result
}

func compareBetaExpectations(t *testing.T, expectations []contract.WebhookExpectation, webhooks []contract.WebhookEvaluation) {
	t.Helper()
	if got, want := len(expectations), len(webhooks); got != want {
		t.Fatalf("expectation count = %d, want %d", got, want)
	}
	for index, result := range webhooks {
		expectation := expectations[index]
		if got, want := result.WebhookName, expectation.WebhookName; got != want {
			t.Errorf("Webhook[%d] name = %q, want %q", index, got, want)
		}
		if got, want := result.Determination, expectation.Determination; got != want {
			t.Errorf("Webhook %q determination = %q, want %q", result.WebhookName, got, want)
		}
		if result.Outcome == nil || expectation.Outcome == nil || *result.Outcome != *expectation.Outcome {
			t.Errorf("Webhook %q outcome = %v, want %v", result.WebhookName, result.Outcome, expectation.Outcome)
		}
		if got, ok := betaTerminalReason(result.Trace); !ok || got != expectation.TerminalReasonCode {
			t.Errorf("Webhook %q terminal reason = %q present=%t, want %q", result.WebhookName, got, ok, expectation.TerminalReasonCode)
		}
	}
}

func betaTerminalReason(trace []contract.TraceStep) (contract.ReasonCode, bool) {
	var reason contract.ReasonCode
	found := false
	for _, step := range trace {
		if !step.Terminal {
			continue
		}
		if found {
			return "", false
		}
		reason = step.ReasonCode
		found = true
	}
	return reason, found
}

func prepareBetaNamespace(t *testing.T, ctx context.Context, harness *Harness, input *contract.Scenario) {
	t.Helper()
	if input.ExternalContext == nil || input.ExternalContext.Namespace == nil {
		t.Fatal("beta Scenario namespace fixture = absent, want exact Namespace")
	}
	namespace := input.ExternalContext.Namespace.DeepCopy()
	if got, want := namespace.Name, input.Request.Namespace; got != want {
		t.Fatalf("namespace fixture name = %q, request namespace %q", got, want)
	}
	if _, err := harness.Kubernetes.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create beta Namespace %s: %v", namespace.Name, err)
	}
	harness.track(func(ctx context.Context) error {
		err := harness.Kubernetes.CoreV1().Namespaces().Delete(ctx, namespace.Name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete beta Namespace %s: %w", namespace.Name, err)
		}
		return nil
	})
}

func installBetaConfiguration(
	ctx context.Context,
	harness *Harness,
	input *contract.Scenario,
	recorders []*Recorder,
) (func(context.Context) error, error) {
	switch {
	case input.Configuration.Validating != nil && input.Configuration.Mutating == nil:
		object := input.Configuration.Validating.DeepCopy()
		if len(object.Webhooks) != len(recorders) {
			return nil, &ObservationContractError{Err: fmt.Errorf("validating Webhook count %d does not match recorder count %d", len(object.Webhooks), len(recorders))}
		}
		for index := range object.Webhooks {
			clientConfig, err := recorderClientConfig(recorders[index])
			if err != nil {
				return nil, err
			}
			object.Webhooks[index].ClientConfig = clientConfig
		}
		if _, err := harness.Kubernetes.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(ctx, object, metav1.CreateOptions{}); err != nil {
			return nil, &SetupError{Stage: SetupResource, Err: fmt.Errorf("create beta validating Webhook configuration %s: %w", object.Name, err)}
		}
		cleanup := func(ctx context.Context) error {
			err := harness.Kubernetes.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(ctx, object.Name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete beta validating Webhook configuration %s: %w", object.Name, err)
			}
			return nil
		}
		harness.track(cleanup)
		return cleanup, nil
	case input.Configuration.Mutating != nil && input.Configuration.Validating == nil:
		object := input.Configuration.Mutating.DeepCopy()
		if len(object.Webhooks) != len(recorders) {
			return nil, &ObservationContractError{Err: fmt.Errorf("mutating Webhook count %d does not match recorder count %d", len(object.Webhooks), len(recorders))}
		}
		for index := range object.Webhooks {
			clientConfig, err := recorderClientConfig(recorders[index])
			if err != nil {
				return nil, err
			}
			object.Webhooks[index].ClientConfig = clientConfig
		}
		if _, err := harness.Kubernetes.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(ctx, object, metav1.CreateOptions{}); err != nil {
			return nil, &SetupError{Stage: SetupResource, Err: fmt.Errorf("create beta mutating Webhook configuration %s: %w", object.Name, err)}
		}
		cleanup := func(ctx context.Context) error {
			err := harness.Kubernetes.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(ctx, object.Name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete beta mutating Webhook configuration %s: %w", object.Name, err)
			}
			return nil
		}
		harness.track(cleanup)
		return cleanup, nil
	default:
		return nil, &ObservationContractError{Err: fmt.Errorf("beta Scenario must contain exactly one Webhook configuration")}
	}
}

func executeBetaRequest(ctx context.Context, harness *Harness, input *contract.Scenario) error {
	switch input.Request.Kind {
	case metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}:
		var object appsv1.Deployment
		if err := json.Unmarshal(input.Request.Object, &object); err != nil {
			return &ObservationContractError{Err: fmt.Errorf("decode beta Deployment: %w", err)}
		}
		_, err := harness.Kubernetes.AppsV1().Deployments(input.Request.Namespace).Create(ctx, &object, metav1.CreateOptions{})
		if err == nil {
			harness.track(func(ctx context.Context) error {
				deleteErr := harness.Kubernetes.AppsV1().Deployments(input.Request.Namespace).Delete(ctx, object.Name, metav1.DeleteOptions{})
				if deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
					return fmt.Errorf("delete beta Deployment %s/%s: %w", input.Request.Namespace, object.Name, deleteErr)
				}
				return nil
			})
		}
		return err
	case metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}:
		var object corev1.Pod
		if err := json.Unmarshal(input.Request.Object, &object); err != nil {
			return &ObservationContractError{Err: fmt.Errorf("decode beta Pod: %w", err)}
		}
		_, err := harness.Kubernetes.CoreV1().Pods(input.Request.Namespace).Create(ctx, &object, metav1.CreateOptions{})
		if err == nil {
			harness.track(func(ctx context.Context) error {
				deleteErr := harness.Kubernetes.CoreV1().Pods(input.Request.Namespace).Delete(ctx, object.Name, metav1.DeleteOptions{})
				if deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
					return fmt.Errorf("delete beta Pod %s/%s: %w", input.Request.Namespace, object.Name, deleteErr)
				}
				return nil
			})
		}
		return err
	default:
		return &ObservationContractError{Err: fmt.Errorf("unsupported beta request kind %s", input.Request.Kind.String())}
	}
}

func verifyBetaAdmissionReview(t *testing.T, webhookName string, input *contract.Scenario, review RecordedReview) {
	t.Helper()
	if got, want := review.Operation, input.Request.Operation; got != want {
		t.Errorf("Webhook %q AdmissionReview operation = %q, want %q", webhookName, got, want)
	}
	if got, want := review.Resource, input.Request.Resource; got != want {
		t.Errorf("Webhook %q AdmissionReview resource = %+v, want %+v", webhookName, got, want)
	}
	if got, want := review.Namespace, input.Request.Namespace; got != want {
		t.Errorf("Webhook %q AdmissionReview namespace = %q, want %q", webhookName, got, want)
	}
	if got, want := review.Name, input.Request.Name; got != want {
		t.Errorf("Webhook %q AdmissionReview name = %q, want %q", webhookName, got, want)
	}
}
