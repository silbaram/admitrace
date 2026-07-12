package fixture

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestAuthorizerHasNoRuntimeClusterOrNetworkDependencies(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "authorizer.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("ParseFile(authorizer.go) error = %v", err)
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %q: %v", spec.Path.Value, err)
		}
		if forbiddenAuthorizerImport(path) {
			t.Errorf("authorizer.go imports forbidden runtime dependency %q", path)
		}
	}
}

func forbiddenAuthorizerImport(path string) bool {
	if path == "net" || strings.HasPrefix(path, "net/") {
		return true
	}
	if strings.HasPrefix(path, "k8s.io/client-go") || strings.HasPrefix(path, "k8s.io/api/authorization") {
		return true
	}
	if strings.HasPrefix(path, "k8s.io/apiserver") {
		return path != "k8s.io/apiserver/pkg/authorization/authorizer"
	}
	return strings.HasPrefix(path, "google.golang.org/grpc")
}
