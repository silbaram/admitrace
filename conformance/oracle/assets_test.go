package oracle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveAssets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test executable uses a POSIX shell")
	}
	tests := []struct {
		name        string
		version     string
		omitEtcd    bool
		wantStage   SetupStage
		wantSuccess bool
	}{
		{name: "exact version", version: "Kubernetes v1.36.2", wantSuccess: true},
		{name: "wrong patch", version: "Kubernetes v1.36.1", wantStage: SetupAssets},
		{name: "missing etcd", version: "Kubernetes v1.36.2", omitEtcd: true, wantStage: SetupAssets},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			writeExecutable(t, filepath.Join(directory, "kube-apiserver"), "#!/bin/sh\nprintf '%s\\n' '"+test.version+"'\n")
			if !test.omitEtcd {
				writeExecutable(t, filepath.Join(directory, "etcd"), "#!/bin/sh\nexit 0\n")
			}

			got, err := ResolveAssets(context.Background(), directory)
			if test.wantSuccess {
				if err != nil {
					t.Fatalf("ResolveAssets() error = %v", err)
				}
				want, err := filepath.Abs(directory)
				if err != nil {
					t.Fatalf("filepath.Abs() error = %v", err)
				}
				if got != want {
					t.Errorf("ResolveAssets() = %q, want %q", got, want)
				}
				return
			}
			var setupError *SetupError
			if !errors.As(err, &setupError) {
				t.Fatalf("ResolveAssets() error = %T, want *SetupError", err)
			}
			if setupError.Stage != test.wantStage {
				t.Errorf("SetupError.Stage = %q, want %q", setupError.Stage, test.wantStage)
			}
		})
	}
}

func TestResolveAssetsRequiresExplicitDirectory(t *testing.T) {
	_, err := ResolveAssets(context.Background(), "")
	var setupError *SetupError
	if !errors.As(err, &setupError) {
		t.Fatalf("ResolveAssets() error = %T, want *SetupError", err)
	}
	if setupError.Stage != SetupAssets {
		t.Errorf("SetupError.Stage = %q, want %q", setupError.Stage, SetupAssets)
	}
}

func TestResolveAssetsRequiresContext(t *testing.T) {
	_, err := ResolveAssets(nil, t.TempDir())
	var setupError *SetupError
	if !errors.As(err, &setupError) {
		t.Fatalf("ResolveAssets() error = %T, want *SetupError", err)
	}
	if setupError.Stage != SetupAssets {
		t.Errorf("SetupError.Stage = %q, want %q", setupError.Stage, SetupAssets)
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
