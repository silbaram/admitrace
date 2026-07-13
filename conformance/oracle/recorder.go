package oracle

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"sync"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RecordedReview is the minimal AdmissionReview evidence retained by the oracle.
type RecordedReview struct {
	UID       string
	Operation admissionv1.Operation
	Resource  metav1.GroupVersionResource
	Namespace string
	Name      string
}

// Recorder is a loopback TLS AdmissionReview Webhook.
type Recorder struct {
	server   *http.Server
	listener net.Listener
	url      string
	caBundle []byte
	cancel   context.CancelFunc
	wait     sync.WaitGroup
	mu       sync.Mutex
	calls    []RecordedReview
	serveErr error
	closeErr error
}

// StartRecorder starts a loopback TLS Webhook with an explicit cancellation path.
func StartRecorder(ctx context.Context) (*Recorder, error) {
	if ctx == nil {
		return nil, &SetupError{Stage: SetupTLS, Err: errors.New("context is required")}
	}
	certificate, caBundle, err := newLoopbackCertificate()
	if err != nil {
		return nil, &SetupError{Stage: SetupTLS, Err: fmt.Errorf("create loopback certificate: %w", err)}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, &SetupError{Stage: SetupTLS, Err: fmt.Errorf("listen on loopback: %w", err)}
	}
	serverContext, cancel := context.WithCancel(ctx)
	recorder := &Recorder{
		listener: listener,
		url:      "https://" + listener.Addr().String(),
		caBundle: caBundle,
		cancel:   cancel,
	}
	recorder.server = &http.Server{
		Handler:           http.HandlerFunc(recorder.serveHTTP),
		ReadHeaderTimeout: 5 * time.Second,
	}
	tlsListener := tls.NewListener(listener, &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	})

	recorder.wait.Add(1)
	go func() {
		defer recorder.wait.Done()
		if err := recorder.server.Serve(tlsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			recorder.recordServeError(fmt.Errorf("serve loopback Webhook: %w", err))
			cancel()
		}
	}()
	recorder.wait.Add(1)
	go func() {
		defer recorder.wait.Done()
		<-serverContext.Done()
		if err := recorder.server.Close(); err != nil {
			recorder.recordCloseError(fmt.Errorf("close loopback Webhook: %w", err))
		}
	}()
	return recorder, nil
}

// URL returns the loopback Webhook URL.
func (r *Recorder) URL() string {
	return r.url
}

// CABundle returns a copy of the recorder's serving certificate in PEM form.
func (r *Recorder) CABundle() ([]byte, error) {
	if len(r.caBundle) == 0 {
		return nil, &SetupError{Stage: SetupTLS, Err: errors.New("loopback certificate is unavailable")}
	}
	return append([]byte(nil), r.caBundle...), nil
}

// Snapshot returns a stable copy of all received calls.
func (r *Recorder) Snapshot() []RecordedReview {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RecordedReview(nil), r.calls...)
}

// Err returns an unexpected serving or shutdown failure as a TLS setup error.
func (r *Recorder) Err() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.serveErr == nil && r.closeErr == nil {
		return nil
	}
	return &SetupError{Stage: SetupTLS, Err: errors.Join(r.serveErr, r.closeErr)}
}

// Close stops the TLS Webhook, waits for its goroutines, and returns shutdown failures.
func (r *Recorder) Close() error {
	if r != nil && r.server != nil {
		r.cancel()
		r.wait.Wait()
	}
	return r.Err()
}

func (r *Recorder) recordServeError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.serveErr = err
}

func (r *Recorder) recordCloseError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeErr = err
}

func (r *Recorder) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var review admissionv1.AdmissionReview
	if err := json.NewDecoder(request.Body).Decode(&review); err != nil || review.Request == nil {
		http.Error(writer, "invalid AdmissionReview", http.StatusBadRequest)
		return
	}

	recorded := RecordedReview{
		UID:       string(review.Request.UID),
		Operation: review.Request.Operation,
		Resource:  review.Request.Resource,
		Namespace: review.Request.Namespace,
		Name:      review.Request.Name,
	}
	r.mu.Lock()
	r.calls = append(r.calls, recorded)
	r.mu.Unlock()

	response := admissionv1.AdmissionReview{
		TypeMeta: review.TypeMeta,
		Response: &admissionv1.AdmissionResponse{
			UID:     review.Request.UID,
			Allowed: true,
		},
	}
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		return
	}
}

func newLoopbackCertificate() (tls.Certificate, []byte, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("generate key: %w", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "admitrace-envtest-loopback"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("create certificate: %w", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("marshal private key: %w", err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("load key pair: %w", err)
	}
	return certificate, certificatePEM, nil
}
