package kube136

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestSelectorMatcherUsesOnlyApprovedStagingDependencies(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "selectors.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("ParseFile(selectors.go) error = %v", err)
	}
	foundSelectorSemantics := false
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %q: %v", spec.Path.Value, err)
		}
		if path == "k8s.io/apimachinery/pkg/apis/meta/v1" {
			foundSelectorSemantics = true
		}
		if forbiddenSelectorMatcherImport(path) {
			t.Errorf("selectors.go imports forbidden dependency %q", path)
		}
	}
	if !foundSelectorSemantics {
		t.Error("selectors.go does not import Kubernetes staging selector semantics")
	}
}

func forbiddenSelectorMatcherImport(path string) bool {
	return path == "net" ||
		strings.HasPrefix(path, "net/") ||
		strings.HasPrefix(path, "k8s.io/client-go") ||
		strings.HasPrefix(path, "k8s.io/kubernetes")
}
