//go:build conformance

package celauthorizer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/silbaram/admitrace/conformance/oracle"
	"github.com/silbaram/admitrace/conformance/parity"
	"github.com/silbaram/admitrace/internal/contract"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKubeAPIServerObservations(t *testing.T) {
	harness := startHarness(t)
	tests := []struct {
		name          string
		conditions    []admissionregistrationv1.MatchCondition
		failurePolicy admissionregistrationv1.FailurePolicyType
	}{
		{name: "cel-no-conditions"},
		{name: "cel-true", conditions: conditions(condition("true", "true"))},
		{name: "cel-false", conditions: conditions(condition("false", "false"))},
		{name: "cel-false-overrides-error", conditions: conditions(
			condition("error", "object.metadata.labels['missing'] == 'required'"),
			condition("false", "false"),
		)},
		{name: "cel-runtime-fail", conditions: conditions(condition("runtime", "object.metadata.labels['missing'] == 'required'"))},
		{name: "cel-runtime-ignore", conditions: conditions(condition("runtime", "object.metadata.labels['missing'] == 'required'")), failurePolicy: admissionregistrationv1.Ignore},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testCase := findCase(t, test.name)
			if testCase.OracleType != parity.OracleKubeAPIServerObservation {
				t.Fatalf("Scenario oracleType = %q, want %q", testCase.OracleType, parity.OracleKubeAPIServerObservation)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			recorder, err := oracle.StartRecorder(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer closeRecorder(t, recorder)
			caBundle, err := recorder.CABundle()
			if err != nil {
				t.Fatal(err)
			}
			configurationName := fmt.Sprintf("cel-advanced-%d.oracle.admitrace.io", index)
			cleanupConfiguration, err := harness.InstallConfiguration(ctx, oracle.Configuration{
				Kind:            oracle.ValidatingConfiguration,
				Name:            configurationName,
				WebhookName:     configurationName,
				URL:             recorder.URL(),
				CABundle:        caBundle,
				Version:         "v1",
				Resource:        "configmaps",
				MatchPolicy:     admissionregistrationv1.Exact,
				FailurePolicy:   test.failurePolicy,
				MatchConditions: test.conditions,
			})
			if err != nil {
				t.Fatal(err)
			}
			registerConfigurationCleanup(t, cleanupConfiguration)

			before := len(recorder.Snapshot())
			objectName := fmt.Sprintf("advanced-%d", index)
			_, requestErr := harness.Kubernetes.CoreV1().ConfigMaps("default").Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: objectName},
			}, metav1.CreateOptions{})
			calls := recorder.Snapshot()
			if err := recorder.Err(); err != nil {
				t.Fatal(err)
			}
			lastReview := oracle.RecordedReview{}
			if len(calls) > before {
				lastReview = calls[len(calls)-1]
			}
			observation, err := oracle.Observe(before, len(calls), lastReview, requestErr)
			if err != nil {
				t.Fatal(err)
			}
			if err := oracle.Compare(observationKind(t, testCase.Expected.Outcome), observation); err != nil {
				t.Fatalf("Scenario %s: %v", test.name, err)
			}
			if requestErr == nil {
				if err := harness.Kubernetes.CoreV1().ConfigMaps("default").Delete(ctx, objectName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
					t.Errorf("delete observed ConfigMap: %v", err)
				}
			}
		})
	}
}

func registerConfigurationCleanup(t *testing.T, cleanup func(context.Context) error) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cleanup(ctx); err != nil {
			t.Errorf("cleanup configuration: %v", err)
		}
	})
}

func findCase(t *testing.T, name string) parity.Case {
	t.Helper()
	for _, testCase := range cases() {
		if testCase.Scenario.Metadata.Name == name {
			return testCase
		}
	}
	t.Fatalf("Scenario %q not found", name)
	return parity.Case{}
}

func observationKind(t *testing.T, outcome *contract.Outcome) oracle.ObservationKind {
	t.Helper()
	if outcome == nil {
		t.Fatal("observed Scenario outcome must not be nil")
	}
	switch *outcome {
	case contract.OutcomeCalled:
		return oracle.ObservationCalled
	case contract.OutcomeSkipped:
		return oracle.ObservationSkipped
	case contract.OutcomeRejectedBeforeCall:
		return oracle.ObservationRejectedBeforeCall
	default:
		t.Fatalf("observed Scenario outcome = %q, want registered outcome", *outcome)
		return ""
	}
}

func startHarness(t *testing.T) *oracle.Harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	harness, err := oracle.Start(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer cancel()
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := harness.Cleanup(cleanupContext); err != nil {
			t.Errorf("cleanup harness: %v", err)
		}
	})
	return harness
}

func closeRecorder(t *testing.T, recorder *oracle.Recorder) {
	t.Helper()
	if err := recorder.Close(); err != nil {
		t.Errorf("close recorder: %v", err)
	}
}
