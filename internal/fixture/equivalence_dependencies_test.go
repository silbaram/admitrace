package fixture

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestEquivalentResourceMapperHasNoRuntimeClusterDependencies(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "equivalence.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("ParseFile(equivalence.go) error = %v", err)
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %q: %v", spec.Path.Value, err)
		}
		if forbiddenEquivalenceImport(path) {
			t.Errorf("equivalence.go imports forbidden runtime dependency %q", path)
		}
	}
}

func forbiddenEquivalenceImport(path string) bool {
	if path == "net" || strings.HasPrefix(path, "net/") {
		return true
	}
	if path == "k8s.io/apimachinery/pkg/api/meta" {
		return true
	}
	if strings.HasPrefix(path, "k8s.io/apimachinery/pkg/runtime") && path != "k8s.io/apimachinery/pkg/runtime/schema" {
		return true
	}
	return strings.HasPrefix(path, "k8s.io/apiserver") ||
		strings.HasPrefix(path, "k8s.io/client-go") ||
		strings.Contains(path, "/discovery")
}
