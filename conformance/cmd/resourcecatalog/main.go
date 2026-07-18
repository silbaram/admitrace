package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/silbaram/admitrace/conformance/oracle"
	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/resourcecatalog"
	"k8s.io/client-go/discovery"
)

func main() {
	output := flag.String("output", "", "write the canonical catalog to this path instead of stdout")
	verify := flag.String("verify", "", "compare regenerated discovery with this committed catalog")
	flag.Parse()
	if err := run(*output, *verify); err != nil {
		fmt.Fprintln(os.Stderr, "resource catalog generation:", err)
		os.Exit(1)
	}
}

func run(output, verify string) error {
	if output != "" && verify != "" {
		return errors.New("output and verify are mutually exclusive")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness, err := oracle.Start(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if cleanupErr := harness.Cleanup(context.Background()); cleanupErr != nil {
			fmt.Fprintln(os.Stderr, "resource catalog cleanup:", cleanupErr)
		}
	}()

	client, err := discovery.NewDiscoveryClientForConfig(harness.Config)
	if err != nil {
		return fmt.Errorf("create discovery client: %w", err)
	}
	version, err := client.ServerVersion()
	if err != nil {
		return fmt.Errorf("read server version: %w", err)
	}
	if version.GitVersion != "v"+kube136.KubernetesVersion {
		return fmt.Errorf("server version got %q, want %q", version.GitVersion, "v"+kube136.KubernetesVersion)
	}
	_, lists, err := client.ServerGroupsAndResources()
	if err != nil {
		return fmt.Errorf("discover API resources: %w", err)
	}
	catalog, err := resourcecatalog.Generate(
		kube136.ProfileID,
		kube136.KubernetesVersion,
		lists,
		resourcecatalog.BuiltInGroupVersions(lists),
	)
	if err != nil {
		return err
	}
	encoded, err := resourcecatalog.Marshal(catalog)
	if err != nil {
		return err
	}
	if verify != "" {
		committedBytes, err := os.ReadFile(verify)
		if err != nil {
			return fmt.Errorf("read committed catalog: %w", err)
		}
		committed, err := resourcecatalog.Parse(committedBytes, kube136.ProfileID, kube136.KubernetesVersion)
		if err != nil {
			return fmt.Errorf("validate committed catalog: %w", err)
		}
		if err := resourcecatalog.Compare(committed, catalog); err != nil {
			return err
		}
		if !bytes.Equal(committedBytes, encoded) {
			return errors.New("resource catalog formatting is not byte stable")
		}
		return nil
	}
	if output == "" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".resource-catalog-*")
	if err != nil {
		return fmt.Errorf("create temporary catalog: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := errors.Join(writeAll(temporary, encoded), temporary.Chmod(0o644), temporary.Close()); err != nil {
		return fmt.Errorf("write temporary catalog: %w", err)
	}
	if err := os.Rename(temporaryName, output); err != nil {
		return fmt.Errorf("publish catalog: %w", err)
	}
	return nil
}

func writeAll(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}
