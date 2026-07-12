package kube136

import (
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestMatchConditionsUseApprovedKubernetesCELDependencies(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "matchconditions.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("ParseFile(matchconditions.go) error = %v", err)
	}
	required := map[string]bool{
		"k8s.io/apiserver/pkg/admission/plugin/cel":                     false,
		"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions": false,
		"k8s.io/apiserver/pkg/cel/environment":                          false,
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %q: %v", spec.Path.Value, err)
		}
		if _, ok := required[path]; ok {
			required[path] = true
		}
		if forbiddenMatchConditionsImport(path) {
			t.Errorf("matchconditions.go imports forbidden dependency %q", path)
		}
	}
	requiredPaths := make([]string, 0, len(required))
	for path := range required {
		requiredPaths = append(requiredPaths, path)
	}
	sort.Strings(requiredPaths)
	for _, path := range requiredPaths {
		found := required[path]
		if !found {
			t.Errorf("matchconditions.go does not import required Kubernetes staging package %q", path)
		}
	}
}

func TestCELGoRemainsAnIndirectKubernetesDependency(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("../../../go.mod")
	if err != nil {
		t.Fatalf("ReadFile(go.mod) error = %v", err)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.Contains(line, "github.com/google/cel-go ") && !strings.Contains(line, "// indirect") {
			t.Errorf("go.mod directly requires cel-go: %q", strings.TrimSpace(line))
		}
	}
}

func forbiddenMatchConditionsImport(path string) bool {
	return path == "net" ||
		strings.HasPrefix(path, "net/") ||
		strings.HasPrefix(path, "github.com/google/cel-go") ||
		strings.HasPrefix(path, "k8s.io/client-go") ||
		strings.HasPrefix(path, "k8s.io/kubernetes")
}
