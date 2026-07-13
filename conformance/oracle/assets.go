package oracle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// KubernetesVersion is the exact control-plane version used by the oracle.
	KubernetesVersion = "1.36.2"
	// EnvtestVersion is the exact controller-runtime/envtest version used by this module.
	EnvtestVersion = "0.24.1"
)

var requiredAssetNames = []string{"etcd", "kube-apiserver"}

// ResolveAssets validates the explicitly supplied envtest asset directory.
func ResolveAssets(ctx context.Context, directory string) (string, error) {
	if ctx == nil {
		return "", &SetupError{Stage: SetupAssets, Err: errors.New("context is required")}
	}
	directory = filepath.Clean(directory)
	if directory == "." || directory == "" {
		return "", &SetupError{Stage: SetupAssets, Err: errors.New("KUBEBUILDER_ASSETS is required")}
	}

	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", &SetupError{Stage: SetupAssets, Err: fmt.Errorf("resolve asset directory: %w", err)}
	}
	for _, name := range requiredAssetNames {
		if err := requireExecutable(filepath.Join(absolute, name)); err != nil {
			return "", &SetupError{Stage: SetupAssets, Err: err}
		}
	}

	versionContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(versionContext, filepath.Join(absolute, "kube-apiserver"), "--version").CombinedOutput()
	if err != nil {
		return "", &SetupError{Stage: SetupAssets, Err: fmt.Errorf("read kube-apiserver version: %w", err)}
	}
	want := "Kubernetes v" + KubernetesVersion
	if got := strings.TrimSpace(string(output)); got != want {
		return "", &SetupError{Stage: SetupAssets, Err: fmt.Errorf("kube-apiserver version got %q, want %q", got, want)}
	}
	return absolute, nil
}

func requireExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("required asset %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("required asset %s is not executable", path)
	}
	return nil
}
