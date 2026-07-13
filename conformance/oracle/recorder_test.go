package oracle

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestRecorderReceivesAdmissionReviewOverTLS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	recorder, err := StartRecorder(ctx)
	if err != nil {
		cancel()
		t.Fatalf("StartRecorder() error = %v", err)
	}
	t.Cleanup(func() {
		cancel()
		if err := recorder.Close(); err != nil {
			t.Errorf("Recorder.Close() error = %v", err)
		}
	})

	caBundle, err := recorder.CABundle()
	if err != nil {
		t.Fatalf("CABundle() error = %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caBundle) {
		t.Fatal("AppendCertsFromPEM() = false, want true")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}
	review := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
		Request: &admissionv1.AdmissionRequest{
			UID:       types.UID("oracle-uid"),
			Operation: admissionv1.Create,
			Resource:  metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			Namespace: "default",
			Name:      "example",
		},
	}
	body, err := json.Marshal(review)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	response, err := client.Post(recorder.URL(), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	responseBody, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		t.Fatalf("io.ReadAll() error = %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("response.Body.Close() error = %v", closeErr)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d body = %q, want 200", response.StatusCode, responseBody)
	}

	calls := recorder.Snapshot()
	if len(calls) != 1 {
		t.Fatalf("len(Snapshot()) = %d, want 1", len(calls))
	}
	if calls[0].UID != "oracle-uid" {
		t.Errorf("RecordedReview.UID = %q, want %q", calls[0].UID, "oracle-uid")
	}
}

func TestRecorderReportsUnexpectedServeFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder, err := StartRecorder(ctx)
	if err != nil {
		t.Fatalf("StartRecorder() error = %v", err)
	}
	if err := recorder.listener.Close(); err != nil {
		t.Fatalf("listener.Close() error = %v", err)
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for recorder.Err() == nil {
		select {
		case <-deadline.C:
			t.Fatal("Recorder.Err() = nil, want non-nil after listener failure")
		case <-ticker.C:
		}
	}
	err = recorder.Close()
	var setupError *SetupError
	if !errors.As(err, &setupError) {
		t.Fatalf("Recorder.Close() error = %T, want *SetupError", err)
	}
	if setupError.Stage != SetupTLS {
		t.Errorf("SetupError.Stage = %q, want %q", setupError.Stage, SetupTLS)
	}
}
