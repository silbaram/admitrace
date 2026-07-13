package oracle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Harness owns one isolated envtest control plane and its registered resources.
type Harness struct {
	environment *envtest.Environment
	Config      *rest.Config
	Kubernetes  kubernetes.Interface
	Dynamic     dynamic.Interface
	Extensions  apiextensionsclient.Interface

	mu       sync.Mutex
	cleanups []func(context.Context) error
}

// Start launches envtest using only explicitly provisioned Kubernetes 1.36.2 assets.
func Start(ctx context.Context) (*Harness, error) {
	assets, err := ResolveAssets(ctx, os.Getenv("KUBEBUILDER_ASSETS"))
	if err != nil {
		return nil, err
	}
	useExistingCluster := false
	environment := &envtest.Environment{
		ControlPlane: envtest.ControlPlane{
			APIServer: &envtest.APIServer{Path: filepath.Join(assets, "kube-apiserver")},
			Etcd:      &envtest.Etcd{Path: filepath.Join(assets, "etcd")},
		},
		BinaryAssetsDirectory:       assets,
		DownloadBinaryAssets:        false,
		DownloadBinaryAssetsVersion: KubernetesVersion,
		UseExistingCluster:          &useExistingCluster,
	}
	config, err := environment.Start()
	if err != nil {
		return nil, &SetupError{Stage: SetupControlPlane, Err: fmt.Errorf("start envtest: %w", err)}
	}

	harness := &Harness{environment: environment, Config: rest.CopyConfig(config)}
	if harness.Kubernetes, err = kubernetes.NewForConfig(config); err != nil {
		return nil, harness.abortStart(fmt.Errorf("create kubernetes client: %w", err))
	}
	if harness.Dynamic, err = dynamic.NewForConfig(config); err != nil {
		return nil, harness.abortStart(fmt.Errorf("create dynamic client: %w", err))
	}
	if harness.Extensions, err = apiextensionsclient.NewForConfig(config); err != nil {
		return nil, harness.abortStart(fmt.Errorf("create apiextensions client: %w", err))
	}
	return harness, nil
}

// Cleanup removes tracked test resources in reverse order and stops the control plane.
func (h *Harness) Cleanup(ctx context.Context) error {
	h.mu.Lock()
	cleanups := append([]func(context.Context) error(nil), h.cleanups...)
	h.cleanups = nil
	h.mu.Unlock()

	var failures []error
	for index := len(cleanups) - 1; index >= 0; index-- {
		if err := cleanups[index](ctx); err != nil {
			failures = append(failures, err)
		}
	}
	if h.environment != nil {
		if err := h.environment.Stop(); err != nil {
			failures = append(failures, fmt.Errorf("stop envtest: %w", err))
		}
	}
	return errors.Join(failures...)
}

func (h *Harness) track(cleanup func(context.Context) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanups = append(h.cleanups, cleanup)
}

func (h *Harness) abortStart(cause error) error {
	stopErr := h.environment.Stop()
	if stopErr != nil {
		cause = errors.Join(cause, fmt.Errorf("stop failed control plane: %w", stopErr))
	}
	return &SetupError{Stage: SetupControlPlane, Err: cause}
}
