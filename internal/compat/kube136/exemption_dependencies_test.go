package kube136_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestExemptionAdapterUsesStagingBoundary(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "exemption.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parser.ParseFile() error = %v", err)
	}

	wantImport := "k8s.io/apiserver/pkg/admission/plugin/webhook/predicates/rules"
	found := false
	for _, declaration := range file.Decls {
		importDeclaration, ok := declaration.(*ast.GenDecl)
		if !ok || importDeclaration.Tok != token.IMPORT {
			continue
		}
		for _, spec := range importDeclaration.Specs {
			path, err := strconv.Unquote(spec.(*ast.ImportSpec).Path.Value)
			if err != nil {
				t.Fatalf("strconv.Unquote() error = %v", err)
			}
			if path == "k8s.io/kubernetes" {
				t.Fatal("exemption adapter imports forbidden k8s.io/kubernetes root module")
			}
			if path == wantImport {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("exemption adapter does not import %q", wantImport)
	}
}
