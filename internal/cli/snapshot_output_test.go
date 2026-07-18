package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/adapter"
	"github.com/silbaram/admitrace/internal/hydration"
	"github.com/silbaram/admitrace/internal/scenario"
	"github.com/silbaram/admitrace/internal/snapshot"
)

func TestManifestExplainWritesReplayableSnapshotBundle(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	resourcePath := filepath.Join(directory, "resource.yaml")
	configurationPath := filepath.Join(directory, "webhook.yaml")
	target := filepath.Join(directory, "snapshots")
	writeCLIFile(t, resourcePath, `apiVersion: v1
kind: ConfigMap
metadata: {name: example, namespace: team-a}
data: {explicit-value: keep-me}
`)
	writeCLIFile(t, configurationPath, validatingCLIConfiguration("policy.example.com"))

	code, stdout, stderr, hydrationCalls := executeSnapshotExplain(t, []string{
		"explain", "--resource", resourcePath,
		"--webhook-config", configurationPath,
		"--user", "alice", "--user-extra", "tenant=blue",
		"--snapshot-out", target, "-o", "json",
	})
	if code != ExitSuccess || stderr != "" || stdout == "" {
		t.Fatalf("Execute(snapshot) = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if hydrationCalls != 0 {
		t.Errorf("offline snapshot hydration calls = %d, want zero", hydrationCalls)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("os.ReadDir(snapshot target) error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "0001-0001.yaml" {
		t.Fatalf("snapshot entries = %#v", entries)
	}
	data, err := os.ReadFile(filepath.Join(target, entries[0].Name()))
	if err != nil {
		t.Fatalf("os.ReadFile(snapshot) error = %v", err)
	}
	decoded, err := scenario.Decode(data)
	if err != nil {
		t.Fatalf("scenario.Decode(snapshot) error = %v", err)
	}
	if decoded.Request.UserInfo.Username != "alice" || decoded.Request.UserInfo.Extra["tenant"][0] != "blue" || !bytes.Contains(decoded.Request.Object, []byte("keep-me")) {
		t.Errorf("snapshot lost explicit replay input: %#v / %s", decoded.Request.UserInfo, decoded.Request.Object)
	}
}

func TestManifestExplainSecretSnapshotRefusalDoesNotSuppressExplanation(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	resourcePath := filepath.Join(directory, "secret.yaml")
	configurationPath := filepath.Join(directory, "webhook.yaml")
	target := filepath.Join(directory, "snapshots")
	writeCLIFile(t, resourcePath, `apiVersion: v1
kind: Secret
metadata: {name: forbidden, namespace: team-a}
stringData: {password: must-not-write}
`)
	writeCLIFile(t, configurationPath, validatingCLIConfiguration("policy.example.com"))

	code, stdout, stderr, _ := executeSnapshotExplain(t, []string{
		"explain", "--resource", resourcePath, "--webhook-config", configurationPath, "--snapshot-out", target, "-o", "text",
	})
	if code != ExitIncompleteEvaluation || stderr != "" {
		t.Fatalf("Execute(Secret snapshot) = code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"SNAPSHOT_REFUSED", "core/v1 Secret", "do not detect generic secrets in custom resources", "configurationEvaluations:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("Secret explanation missing %q:\n%s", want, stdout)
		}
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Secret snapshot target exists: %v", err)
	}
}

func TestManifestExplainNonEmptySnapshotTargetIsRefusedWithoutChanges(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	resourcePath := filepath.Join(directory, "resource.yaml")
	configurationPath := filepath.Join(directory, "webhook.yaml")
	target := filepath.Join(directory, "snapshots")
	writeCLIFile(t, resourcePath, `apiVersion: v1
kind: ConfigMap
metadata: {name: example, namespace: team-a}
`)
	writeCLIFile(t, configurationPath, validatingCLIConfiguration("policy.example.com"))
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("os.Mkdir(target) error = %v", err)
	}
	writeCLIFile(t, filepath.Join(target, "keep"), "keep")

	code, stdout, stderr, _ := executeSnapshotExplain(t, []string{
		"explain", "--resource", resourcePath, "--webhook-config", configurationPath, "--snapshot-out", target, "-o", "json",
	})
	if code != ExitIncompleteEvaluation || stderr != "" || !strings.Contains(stdout, "SNAPSHOT_REFUSED") {
		t.Fatalf("Execute(non-empty target) = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	data, err := os.ReadFile(filepath.Join(target, "keep"))
	if err != nil || string(data) != "keep" {
		t.Errorf("non-empty target changed: data %q, error %v", data, err)
	}
}

func TestExplainHelpStatesSnapshotPolicyLimit(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute([]string{"explain", "--help"}, strings.NewReader(""), &stdout, &stderr, BuildMetadata{})
	if code != ExitSuccess || stderr.String() != "" {
		t.Fatalf("Execute(help) = code %d, stderr %q", code, stderr.String())
	}
	for _, want := range []string{
		"--file is universal",
		"1-based document index",
		"CREATE-only resource mode",
		"generated 1.36.2 built-in catalog",
		"CRDs require verified context discovery",
		"exact Kubernetes 1.36.2",
		"permits GET only",
		"never inferred from kubeconfig",
		"core/v1 Secret",
		"explicit UserInfo",
		"do not detect generic secrets in custom resources",
		"called means routing selected the webhook",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help missing %q:\n%s", want, stdout.String())
		}
	}
	if !strings.Contains(snapshot.PolicyNotice, "without field redaction") {
		t.Errorf("PolicyNotice = %q", snapshot.PolicyNotice)
	}
}

func executeSnapshotExplain(t *testing.T, args []string) (ExitCode, string, string, int) {
	t.Helper()
	dependencies := defaultCommandDependencies()
	hydrationCalls := 0
	dependencies.prepareHydration = func(context.Context, hydration.Options) (*adapter.Hydration, error) {
		hydrationCalls++
		return nil, errors.New("offline mode must not prepare hydration")
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := executeWithDependencies(args, strings.NewReader(""), &stdout, &stderr, BuildMetadata{}, dependencies)
	return code, stdout.String(), stderr.String(), hydrationCalls
}
