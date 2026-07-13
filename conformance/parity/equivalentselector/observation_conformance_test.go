//go:build conformance

package equivalentselector

import (
	"context"
	"testing"
	"time"

	"github.com/silbaram/admitrace/conformance/oracle"
	"github.com/silbaram/admitrace/conformance/parity"
	"github.com/silbaram/admitrace/internal/contract"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestKubeAPIServerObservations(t *testing.T) {
	testCase := findCase(t, "equivalent-explicit-mapping")
	if testCase.OracleType != parity.OracleKubeAPIServerObservation {
		t.Fatalf("Scenario oracleType = %q, want %q", testCase.OracleType, parity.OracleKubeAPIServerObservation)
	}
	harness := startHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resource, cleanupResource, err := harness.InstallEquivalentResource(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupWithDeadline(t, cleanupResource)
	recorder, err := oracle.StartRecorder(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRecorder(t, recorder)
	caBundle, err := recorder.CABundle()
	if err != nil {
		t.Fatal(err)
	}
	cleanupConfiguration, err := harness.InstallConfiguration(ctx, oracle.Configuration{
		Kind:        oracle.ValidatingConfiguration,
		Name:        "equivalent-advanced.oracle.admitrace.io",
		WebhookName: "equivalent-advanced.oracle.admitrace.io",
		URL:         recorder.URL(),
		CABundle:    caBundle,
		Group:       resource.Group,
		Version:     resource.StorageVersion,
		Resource:    resource.Resource,
		MatchPolicy: admissionregistrationv1.Equivalent,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupWithDeadline(t, cleanupConfiguration)

	before := len(recorder.Snapshot())
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": resource.Group + "/" + resource.OtherVersion,
		"kind":       resource.Kind,
		"metadata": map[string]any{
			"name":      "equivalent-explicit-mapping",
			"namespace": "default",
		},
	}}
	_, requestErr := harness.Dynamic.Resource(resource.OtherGVR).Namespace("default").Create(ctx, object, metav1.CreateOptions{})
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
		t.Fatalf("Scenario equivalent-explicit-mapping: %v", err)
	}
	if requestErr == nil {
		if err := harness.Dynamic.Resource(resource.OtherGVR).Namespace("default").Delete(ctx, object.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete observed object: %v", err)
		}
	}
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

func cleanupWithDeadline(t *testing.T, cleanup func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cleanup(ctx); err != nil {
		t.Errorf("cleanup fixture: %v", err)
	}
}

func closeRecorder(t *testing.T, recorder *oracle.Recorder) {
	t.Helper()
	if err := recorder.Close(); err != nil {
		t.Errorf("close recorder: %v", err)
	}
}
