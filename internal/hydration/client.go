package hydration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/manifest"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const maximumVersionResponseBytes = 64 * 1024

// ErrorKind is a stable client preparation and safety taxonomy.
type ErrorKind string

const (
	// ErrorKindKubeconfig identifies failure to load explicit configuration.
	ErrorKindKubeconfig ErrorKind = "kubeconfig"
	// ErrorKindContextNotFound identifies an absent explicitly named context.
	ErrorKindContextNotFound ErrorKind = "context-not-found"
	// ErrorKindClientConfig identifies invalid context, cluster, or auth configuration.
	ErrorKindClientConfig ErrorKind = "client-config"
	// ErrorKindTransport identifies failure to prepare or use the protected transport.
	ErrorKindTransport ErrorKind = "transport"
	// ErrorKindServerVersion identifies an unreadable Kubernetes version response.
	ErrorKindServerVersion ErrorKind = "server-version"
	// ErrorKindProfileMismatch identifies a server outside exact Kubernetes 1.36.2.
	ErrorKindProfileMismatch ErrorKind = "profile-mismatch"
	// ErrorKindMethodForbidden identifies a request rejected by the GET-only guard.
	ErrorKindMethodForbidden ErrorKind = "method-forbidden"
)

// IsValid reports whether kind belongs to the hydration error taxonomy.
func (kind ErrorKind) IsValid() bool {
	switch kind {
	case ErrorKindKubeconfig,
		ErrorKindContextNotFound,
		ErrorKindClientConfig,
		ErrorKindTransport,
		ErrorKindServerVersion,
		ErrorKindProfileMismatch,
		ErrorKindMethodForbidden:
		return true
	default:
		return false
	}
}

// Error reports a stable kind without copying kubeconfig paths, credentials,
// server URLs, or response bodies into its display text.
type Error struct {
	Kind            ErrorKind
	Operation       string
	ObservedVersion string
	ProfileMatch    *manifest.ProfileMatch
	Err             error
}

// Error returns a stable, redacted summary.
func (err *Error) Error() string {
	if err == nil {
		return "Kubernetes hydration error"
	}
	message := "Kubernetes hydration " + string(err.Kind)
	if err.Operation != "" {
		message += ": " + err.Operation
	}
	if err.Kind == ErrorKindProfileMismatch && err.ObservedVersion != "" {
		message += fmt.Sprintf(": observed %q, supported %q", err.ObservedVersion, kube136.KubernetesVersion)
	}
	return message
}

// Unwrap exposes the stable contract category and underlying diagnostic cause.
func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// Options selects one kubeconfig context explicitly.
type Options struct {
	Context        string
	KubeconfigPath string
}

// Session is a verified read-only Kubernetes connection. Credential-bearing
// transport and server configuration remain unexported and cannot be encoded
// into result or snapshot data.
type Session struct {
	contextLabel string
	profileMatch manifest.ProfileMatch
	restConfig   *rest.Config
	httpClient   *http.Client
}

// ContextLabel returns a logical label for the explicitly selected context.
func (session *Session) ContextLabel() string {
	if session == nil {
		return ""
	}
	return session.contextLabel
}

// ProfileMatch returns an owned exact-version verification result.
func (session *Session) ProfileMatch() manifest.ProfileMatch {
	if session == nil {
		return manifest.ProfileMatch{}
	}
	return session.profileMatch
}

type configLoader func(Options) (*rest.Config, error)
type transportBuilder func(*rest.Config) (http.RoundTripper, error)

// Factory prepares explicit read-only sessions. Its zero value is not usable;
// callers should use NewFactory.
type Factory struct {
	loadConfig     configLoader
	buildTransport transportBuilder
}

// NewFactory creates a client-go v0.36.2 non-interactive session factory.
func NewFactory() Factory {
	return Factory{loadConfig: loadExplicitConfig, buildTransport: rest.TransportFor}
}

// Connect returns nil without touching kubeconfig or transport when no context
// was explicitly selected. Otherwise it performs only GET /version before
// returning an exact-profile session.
func (factory Factory) Connect(ctx context.Context, options Options) (*Session, error) {
	if options.Context == "" {
		return nil, nil
	}
	if ctx == nil {
		return nil, &Error{Kind: ErrorKindClientConfig, Operation: "require request context", Err: &contract.InvalidInputError{Field: "context", Err: errors.New("context.Context is required")}}
	}
	if factory.loadConfig == nil || factory.buildTransport == nil {
		return nil, &Error{Kind: ErrorKindClientConfig, Operation: "initialize client factory", Err: &contract.InternalError{Operation: "use zero hydration Factory"}}
	}
	config, err := factory.loadConfig(options)
	if err != nil {
		return nil, err
	}
	baseTransport, err := factory.buildTransport(config)
	if err != nil {
		return nil, &Error{Kind: ErrorKindTransport, Operation: "prepare authenticated transport", Err: err}
	}
	guardedTransport, err := NewGETOnlyRoundTripper(baseTransport)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Transport: guardedTransport}
	profileMatch, err := verifyServerVersion(ctx, httpClient, config.Host)
	if err != nil {
		return nil, err
	}

	anonymous := rest.AnonymousClientConfig(config)
	anonymous.Transport = guardedTransport
	return &Session{
		contextLabel: "context:" + options.Context,
		profileMatch: profileMatch,
		restConfig:   anonymous,
		httpClient:   httpClient,
	}, nil
}

func loadExplicitConfig(options Options) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if options.KubeconfigPath != "" {
		rules.ExplicitPath = options.KubeconfigPath
	}
	rawConfig, err := rules.Load()
	if err != nil {
		return nil, &Error{Kind: ErrorKindKubeconfig, Operation: "load explicitly selected kubeconfig", Err: &contract.InvalidInputError{Field: "kubeconfig", Err: err}}
	}
	if _, ok := rawConfig.Contexts[options.Context]; !ok {
		return nil, &Error{Kind: ErrorKindContextNotFound, Operation: "resolve explicitly selected context", Err: &contract.InvalidInputError{Field: "context", Err: errors.New("selected context does not exist")}}
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: options.Context}
	clientConfig := clientcmd.NewNonInteractiveClientConfig(*rawConfig, options.Context, overrides, rules)
	config, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, &Error{Kind: ErrorKindClientConfig, Operation: "resolve non-interactive client configuration", Err: &contract.InvalidInputError{Field: "context", Err: err}}
	}
	return config, nil
}

func verifyServerVersion(ctx context.Context, client *http.Client, host string) (manifest.ProfileMatch, error) {
	endpoint, err := versionEndpoint(host)
	if err != nil {
		return manifest.ProfileMatch{}, &Error{Kind: ErrorKindClientConfig, Operation: "prepare version endpoint", Err: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return manifest.ProfileMatch{}, &Error{Kind: ErrorKindClientConfig, Operation: "prepare version request", Err: err}
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return manifest.ProfileMatch{}, &Error{Kind: ErrorKindTransport, Operation: "GET Kubernetes version", Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return manifest.ProfileMatch{}, &Error{Kind: ErrorKindServerVersion, Operation: fmt.Sprintf("GET Kubernetes version returned HTTP %d", response.StatusCode), Err: errors.New("non-success version response")}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumVersionResponseBytes+1))
	if err != nil {
		return manifest.ProfileMatch{}, &Error{Kind: ErrorKindServerVersion, Operation: "read Kubernetes version response", Err: err}
	}
	if len(body) > maximumVersionResponseBytes {
		return manifest.ProfileMatch{}, &Error{Kind: ErrorKindServerVersion, Operation: "limit Kubernetes version response", Err: errors.New("version response exceeded local limit")}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var info version.Info
	if err := decoder.Decode(&info); err != nil {
		return manifest.ProfileMatch{}, &Error{Kind: ErrorKindServerVersion, Operation: "decode Kubernetes version response", Err: err}
	}
	return validateVersion(info)
}

func validateVersion(info version.Info) (manifest.ProfileMatch, error) {
	observed := strings.TrimPrefix(info.GitVersion, "v")
	if observed == "" {
		observed = "unknown"
	}
	match := manifest.ProfileMatch{
		Status:                    manifest.ProfileMatchVerified,
		Profile:                   contract.Kubernetes136DefaultProfile(),
		ObservedKubernetesVersion: observed,
	}
	if info.GitVersion == "v"+kube136.KubernetesVersion && info.Major == "1" && info.Minor == "36" {
		return match, nil
	}
	match.Status = manifest.ProfileMatchMismatch
	unsupported := &contract.UnsupportedCapabilityError{
		Capability: "Kubernetes compatibility profile",
		Err:        fmt.Errorf("exact server version %q does not match v%s", info.GitVersion, kube136.KubernetesVersion),
	}
	return match, &Error{
		Kind:            ErrorKindProfileMismatch,
		Operation:       "verify exact Kubernetes profile",
		ObservedVersion: observed,
		ProfileMatch:    &match,
		Err:             unsupported,
	}
}

func versionEndpoint(host string) (string, error) {
	parsed, err := url.Parse(host)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", &contract.InvalidInputError{Field: "context", Err: errors.New("selected cluster server URL is invalid")}
	}
	if parsed.User != nil {
		return "", &contract.InvalidInputError{Field: "context", Err: errors.New("selected cluster server URL must not contain user information")}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/version"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

type getOnlyRoundTripper struct {
	base http.RoundTripper
}

// NewGETOnlyRoundTripper wraps a transport with a pre-transmission method
// allowlist. LIST operations use HTTP GET and therefore remain permitted.
func NewGETOnlyRoundTripper(base http.RoundTripper) (http.RoundTripper, error) {
	if base == nil {
		return nil, &Error{Kind: ErrorKindTransport, Operation: "require base transport", Err: &contract.InvalidInputError{Field: "transport", Err: errors.New("base RoundTripper is required")}}
	}
	return &getOnlyRoundTripper{base: base}, nil
}

func (transport *getOnlyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, &Error{Kind: ErrorKindTransport, Operation: "require HTTP request", Err: &contract.InvalidInputError{Field: "request", Err: errors.New("request is required")}}
	}
	if request.Method != http.MethodGet {
		return nil, &Error{
			Kind:      ErrorKindMethodForbidden,
			Operation: "reject non-GET Kubernetes API request",
			Err: &contract.UnsupportedCapabilityError{
				Capability: "Kubernetes API method " + request.Method,
				Err:        errors.New("hydration transport permits GET only"),
			},
		}
	}
	return transport.base.RoundTrip(request)
}
