package kube136

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestExactRuleMatcherUsesOnlyApprovedStagingDependencies(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "rules.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("ParseFile(rules.go) error = %v", err)
	}
	foundStagingMatcher := false
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %q: %v", spec.Path.Value, err)
		}
		if path == "k8s.io/apiserver/pkg/admission/plugin/webhook/predicates/rules" {
			foundStagingMatcher = true
		}
		if forbiddenRuleMatcherImport(path) {
			t.Errorf("rules.go imports forbidden dependency %q", path)
		}
	}
	if !foundStagingMatcher {
		t.Error("rules.go does not import the Kubernetes staging rules matcher")
	}
}

func forbiddenRuleMatcherImport(path string) bool {
	return path == "net" ||
		strings.HasPrefix(path, "net/") ||
		strings.HasPrefix(path, "k8s.io/client-go") ||
		strings.HasPrefix(path, "k8s.io/kubernetes")
}
