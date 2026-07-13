//go:build conformance

package oracle

import (
	"context"
	"fmt"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSmokeObservations(t *testing.T) {
	harness := startConformanceHarness(t)
	tests := []struct {
		name       string
		kind       ConfigurationKind
		expression string
		want       ObservationKind
	}{
		{name: "called", kind: ValidatingConfiguration, expression: "true", want: ObservationCalled},
		{name: "skipped", kind: MutatingConfiguration, expression: "false", want: ObservationSkipped},
		{name: "rejected", kind: ValidatingConfiguration, expression: "object.metadata.labels['missing'] == 'required'", want: ObservationRejectedBeforeCall},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			recorder, err := StartRecorder(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer closeRecorder(t, recorder)
			caBundle, err := recorder.CABundle()
			if err != nil {
				t.Fatal(err)
			}
			configurationName := "smoke-" + test.name + ".oracle.admitrace.io"
			cleanup, err := harness.InstallConfiguration(ctx, Configuration{
				Kind:            test.kind,
				Name:            configurationName,
				WebhookName:     configurationName,
				URL:             recorder.URL(),
				CABundle:        caBundle,
				Version:         "v1",
				Resource:        "configmaps",
				MatchPolicy:     admissionregistrationv1.Exact,
				MatchExpression: test.expression,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cleanupWithDeadline(t, "Webhook configuration", cleanup)

			before := len(recorder.Snapshot())
			objectName := "smoke-" + test.name
			_, requestErr := harness.Kubernetes.CoreV1().ConfigMaps("default").Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: objectName},
			}, metav1.CreateOptions{})
			calls := recorder.Snapshot()
			if err := recorder.Err(); err != nil {
				t.Fatal(err)
			}
			lastReview := RecordedReview{}
			if len(calls) > before {
				lastReview = calls[len(calls)-1]
			}
			observation, err := Observe(before, len(calls), lastReview, requestErr)
			if err != nil {
				t.Fatalf("Observe() error = %v", err)
			}
			if err := Compare(test.want, observation); err != nil {
				t.Fatal(err)
			}
			if requestErr == nil {
				cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				deleteErr := harness.Kubernetes.CoreV1().ConfigMaps("default").Delete(cleanupContext, objectName, metav1.DeleteOptions{})
				cleanupCancel()
				if deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
					t.Errorf("delete ConfigMap %s: %v", objectName, deleteErr)
				}
			}
		})
	}
}

func TestEquivalentAfterExactMiss(t *testing.T) {
	harness := startConformanceHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resource, cleanupResource, err := harness.InstallEquivalentResource(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupWithDeadline(t, "equivalent resource", cleanupResource)

	recorder, err := StartRecorder(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRecorder(t, recorder)
	caBundle, err := recorder.CABundle()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name        string
		matchPolicy admissionregistrationv1.MatchPolicyType
		want        ObservationKind
	}{
		{name: "exact", matchPolicy: admissionregistrationv1.Exact, want: ObservationSkipped},
		{name: "equivalent", matchPolicy: admissionregistrationv1.Equivalent, want: ObservationCalled},
	} {
		t.Run(test.name, func(t *testing.T) {
			configurationName := fmt.Sprintf("%s-%s.oracle.admitrace.io", stableSuffix(t.Name()), test.name)
			cleanupConfiguration, err := harness.InstallConfiguration(ctx, Configuration{
				Kind:        ValidatingConfiguration,
				Name:        configurationName,
				WebhookName: configurationName,
				URL:         recorder.URL(),
				CABundle:    caBundle,
				Group:       resource.Group,
				Version:     resource.StorageVersion,
				Resource:    resource.Resource,
				MatchPolicy: test.matchPolicy,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cleanupWithDeadline(t, "Webhook configuration", cleanupConfiguration)

			before := len(recorder.Snapshot())
			object := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": resource.Group + "/" + resource.OtherVersion,
				"kind":       resource.Kind,
				"metadata": map[string]any{
					"name":      "case-" + test.name,
					"namespace": "default",
				},
			}}
			_, requestErr := harness.Dynamic.Resource(resource.OtherGVR).Namespace("default").Create(ctx, object, metav1.CreateOptions{})
			calls := recorder.Snapshot()
			if err := recorder.Err(); err != nil {
				t.Fatal(err)
			}
			lastReview := RecordedReview{}
			if len(calls) > before {
				lastReview = calls[len(calls)-1]
			}
			observation, err := Observe(before, len(calls), lastReview, requestErr)
			if err != nil {
				t.Fatalf("Observe() error = %v", err)
			}
			if err := Compare(test.want, observation); err != nil {
				t.Fatal(err)
			}
			if requestErr == nil {
				cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				deleteErr := harness.Dynamic.Resource(resource.OtherGVR).Namespace("default").Delete(cleanupContext, object.GetName(), metav1.DeleteOptions{})
				cleanupCancel()
				if deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
					t.Errorf("delete equivalent object %s: %v", object.GetName(), deleteErr)
				}
			}
		})
	}
}

func cleanupWithDeadline(t *testing.T, label string, cleanup func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cleanup(ctx); err != nil {
		t.Errorf("cleanup %s: %v", label, err)
	}
}

func closeRecorder(t *testing.T, recorder *Recorder) {
	t.Helper()
	if err := recorder.Close(); err != nil {
		t.Errorf("close AdmissionReview recorder: %v", err)
	}
}

func startConformanceHarness(t *testing.T) *Harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	harness, err := Start(ctx)
	if err != nil {
		cancel()
		t.Fatalf("start conformance harness: %v", err)
	}
	t.Cleanup(func() {
		defer cancel()
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := harness.Cleanup(cleanupContext); err != nil {
			t.Errorf("cleanup conformance harness: %v", err)
		}
	})
	return harness
}
