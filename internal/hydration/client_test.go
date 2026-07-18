package hydration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/manifest"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestFactoryWithoutContextHasNoSideEffects(t *testing.T) {
	t.Parallel()

	loads := 0
	transports := 0
	factory := Factory{
		loadConfig: func(Options) (*rest.Config, error) {
			loads++
			return nil, errors.New("must not load")
		},
		buildTransport: func(*rest.Config) (http.RoundTripper, error) {
			transports++
			return nil, errors.New("must not build")
		},
	}
	session, err := factory.Connect(context.Background(), Options{KubeconfigPath: "/must/not/be/read"})
	if err != nil || session != nil {
		t.Fatalf("Connect(no context) = (%v, %v), want nil/nil", session, err)
	}
	if loads != 0 || transports != 0 {
		t.Errorf("no-context side effects = loads %d, transports %d; want zero", loads, transports)
	}
}

func TestErrorKindsAreStable(t *testing.T) {
	t.Parallel()

	for _, kind := range []ErrorKind{
		ErrorKindKubeconfig,
		ErrorKindContextNotFound,
		ErrorKindClientConfig,
		ErrorKindTransport,
		ErrorKindServerVersion,
		ErrorKindProfileMismatch,
		ErrorKindMethodForbidden,
	} {
		if !kind.IsValid() {
			t.Errorf("ErrorKind(%q).IsValid() = false", kind)
		}
	}
	if ErrorKind("unknown").IsValid() {
		t.Error("ErrorKind(unknown).IsValid() = true")
	}
}

func TestFactoryLoadsOnlyExplicitContextNonInteractively(t *testing.T) {
	t.Parallel()

	kubeconfigPath := writeKubeconfig(t, clientcmdapi.Config{
		CurrentContext: "implicit",
		Clusters: map[string]*clientcmdapi.Cluster{
			"implicit-cluster": {Server: "https://implicit.invalid"},
			"wanted-cluster":   {Server: "https://wanted.invalid/base"},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"implicit-user": {Token: "implicit-token"},
			"wanted-user":   {Token: "credential-do-not-copy"},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"implicit": {Cluster: "implicit-cluster", AuthInfo: "implicit-user"},
			"wanted":   {Cluster: "wanted-cluster", AuthInfo: "wanted-user"},
		},
	})
	recorder := &recordingRoundTripper{response: versionResponse("v1.36.2", "1", "36")}
	factory := NewFactory()
	factory.buildTransport = func(config *rest.Config) (http.RoundTripper, error) {
		if config.Host != "https://wanted.invalid/base" || config.BearerToken != "credential-do-not-copy" {
			t.Errorf("resolved config host/token did not come from explicit context")
		}
		return recorder, nil
	}

	session, err := factory.Connect(context.Background(), Options{Context: "wanted", KubeconfigPath: kubeconfigPath})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if session.ContextLabel() != "context:wanted" {
		t.Errorf("ContextLabel() = %q, want context:wanted", session.ContextLabel())
	}
	match := session.ProfileMatch()
	if match.Status != manifest.ProfileMatchVerified || match.ObservedKubernetesVersion != "1.36.2" {
		t.Errorf("ProfileMatch() = %#v, want verified 1.36.2", match)
	}
	if err := match.Validate(); err != nil {
		t.Errorf("ProfileMatch validation error = %v", err)
	}
	if len(recorder.requests) != 1 || recorder.requests[0].Method != http.MethodGet || recorder.requests[0].URL.Path != "/base/version" {
		t.Errorf("version requests = %#v, want one GET /base/version", recorder.requests)
	}
	if session.restConfig.BearerToken != "" || len(session.restConfig.TLSClientConfig.CertData) != 0 || session.restConfig.Transport == nil {
		t.Error("session REST config retained serializable credentials or lost protected transport")
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("json.Marshal(Session) error = %v", err)
	}
	if string(encoded) != "{}" {
		t.Errorf("encoded Session = %s, want no credential/server metadata", encoded)
	}
}

func TestFactoryReturnsStableConfigTaxonomy(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "credential-name-must-not-leak")
	_, err := NewFactory().Connect(context.Background(), Options{Context: "wanted", KubeconfigPath: missingPath})
	requireHydrationError(t, err, ErrorKindKubeconfig)
	if strings.Contains(err.Error(), missingPath) || strings.Contains(err.Error(), "credential-name") {
		t.Errorf("display error leaked kubeconfig path: %v", err)
	}

	path := writeKubeconfig(t, clientcmdapi.Config{
		CurrentContext: "implicit",
		Clusters:       map[string]*clientcmdapi.Cluster{"cluster": {Server: "https://cluster.invalid"}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"user": {Token: "secret"}},
		Contexts:       map[string]*clientcmdapi.Context{"implicit": {Cluster: "cluster", AuthInfo: "user"}},
	})
	_, err = NewFactory().Connect(context.Background(), Options{Context: "missing", KubeconfigPath: path})
	requireHydrationError(t, err, ErrorKindContextNotFound)

	invalidPath := writeKubeconfig(t, clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{},
		Contexts:  map[string]*clientcmdapi.Context{"broken": {Cluster: "missing"}},
	})
	_, err = NewFactory().Connect(context.Background(), Options{Context: "broken", KubeconfigPath: invalidPath})
	requireHydrationError(t, err, ErrorKindClientConfig)
}

func TestVersionGateRejectsEveryNonExactServerBeforeOtherRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		gitVersion string
		major      string
		minor      string
	}{
		{name: "older patch", gitVersion: "v1.36.1", major: "1", minor: "36"},
		{name: "newer patch", gitVersion: "v1.36.3", major: "1", minor: "36"},
		{name: "vendor suffix", gitVersion: "v1.36.2-gke.1", major: "1", minor: "36"},
		{name: "build suffix", gitVersion: "v1.36.2+vendor", major: "1", minor: "36"},
		{name: "missing v prefix", gitVersion: "1.36.2", major: "1", minor: "36"},
		{name: "minor suffix", gitVersion: "v1.36.2", major: "1", minor: "36+"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorder := &recordingRoundTripper{response: versionResponse(test.gitVersion, test.major, test.minor)}
			factory := Factory{
				loadConfig: func(Options) (*rest.Config, error) {
					return &rest.Config{Host: "https://server-metadata.invalid"}, nil
				},
				buildTransport: func(*rest.Config) (http.RoundTripper, error) { return recorder, nil },
			}
			session, err := factory.Connect(context.Background(), Options{Context: "explicit"})
			if session != nil || !errors.Is(err, contract.ErrUnsupportedCapability) {
				t.Fatalf("Connect() = (%v, %v), want nil/unsupported", session, err)
			}
			hydrationError := requireHydrationError(t, err, ErrorKindProfileMismatch)
			if hydrationError.ProfileMatch == nil || hydrationError.ProfileMatch.Status != manifest.ProfileMatchMismatch {
				t.Errorf("mismatch profile = %#v, want explicit mismatch", hydrationError.ProfileMatch)
			}
			if len(recorder.requests) != 1 || recorder.requests[0].URL.Path != "/version" {
				t.Errorf("requests before mismatch = %#v, want only /version", recorder.requests)
			}
			if strings.Contains(err.Error(), "server-metadata.invalid") {
				t.Errorf("mismatch error leaked server metadata: %v", err)
			}
		})
	}
}

func TestGETOnlyRoundTripperRejectsWritesBeforeTransmission(t *testing.T) {
	t.Parallel()

	base := &recordingRoundTripper{response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}}
	guarded, err := NewGETOnlyRoundTripper(base)
	if err != nil {
		t.Fatalf("NewGETOnlyRoundTripper() error = %v", err)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead} {
		request, err := http.NewRequest(method, "https://cluster.invalid/api", nil)
		if err != nil {
			t.Fatalf("http.NewRequest(%s) error = %v", method, err)
		}
		_, err = guarded.RoundTrip(request)
		requireHydrationError(t, err, ErrorKindMethodForbidden)
		if !errors.Is(err, contract.ErrUnsupportedCapability) {
			t.Errorf("RoundTrip(%s) error = %v, want unsupported", method, err)
		}
	}
	if len(base.requests) != 0 {
		t.Fatalf("forbidden requests reached base transport: %#v", base.requests)
	}
	request, err := http.NewRequest(http.MethodGet, "https://cluster.invalid/api", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(GET) error = %v", err)
	}
	if _, err := guarded.RoundTrip(request); err != nil {
		t.Fatalf("RoundTrip(GET) error = %v", err)
	}
	if len(base.requests) != 1 || base.requests[0].Method != http.MethodGet {
		t.Errorf("allowed requests = %#v, want one GET", base.requests)
	}
}

func TestFactoryDisplayErrorsRedactTransportSecrets(t *testing.T) {
	t.Parallel()

	const secret = "bearer-token-do-not-copy"
	const server = "https://credential-server.invalid"
	factory := Factory{
		loadConfig: func(Options) (*rest.Config, error) {
			return &rest.Config{Host: server, BearerToken: secret}, nil
		},
		buildTransport: func(*rest.Config) (http.RoundTripper, error) {
			return roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("transport failure containing %s and %s", secret, server)
			}), nil
		},
	}
	_, err := factory.Connect(context.Background(), Options{Context: "explicit"})
	requireHydrationError(t, err, ErrorKindTransport)
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), server) {
		t.Errorf("display error leaked transport metadata: %v", err)
	}
}

type recordingRoundTripper struct {
	requests []*http.Request
	response *http.Response
}

func (transport *recordingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requests = append(transport.requests, request.Clone(request.Context()))
	return transport.response, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func versionResponse(gitVersion, major, minor string) *http.Response {
	body, err := json.Marshal(version.Info{GitVersion: gitVersion, Major: major, Minor: minor})
	if err != nil {
		panic(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
}

func writeKubeconfig(t *testing.T, config clientcmdapi.Config) string {
	t.Helper()
	encoded, err := clientcmd.Write(config)
	if err != nil {
		t.Fatalf("clientcmd.Write() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("os.WriteFile(kubeconfig) error = %v", err)
	}
	return path
}

func requireHydrationError(t *testing.T, err error, want ErrorKind) *Error {
	t.Helper()
	var hydrationError *Error
	if !errors.As(err, &hydrationError) {
		t.Fatalf("error type = %T, want *hydration.Error", err)
	}
	if hydrationError.Kind != want {
		t.Fatalf("error kind = %q, want %q", hydrationError.Kind, want)
	}
	return hydrationError
}
