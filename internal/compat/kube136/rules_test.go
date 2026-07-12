package kube136_test

import (
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestExactRuleMatchesKubernetesSemantics(t *testing.T) {
	t.Parallel()

	appsDeployments := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	coreNamespaces := schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	allScopes := scope(admissionregistrationv1.AllScopes)
	namespacedScope := scope(admissionregistrationv1.NamespacedScope)
	clusterScope := scope(admissionregistrationv1.ClusterScope)

	tests := []struct {
		name        string
		rule        admissionregistrationv1.RuleWithOperations
		operation   admissionv1.Operation
		resource    schema.GroupVersionResource
		subresource string
		namespaced  bool
		want        bool
	}{
		{
			name:      "create",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, allScopes),
			operation: admissionv1.Create,
			resource:  appsDeployments,
			want:      true,
		},
		{
			name:      "update",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Update}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, allScopes),
			operation: admissionv1.Update,
			resource:  appsDeployments,
			want:      true,
		},
		{
			name:      "delete",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Delete}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, allScopes),
			operation: admissionv1.Delete,
			resource:  appsDeployments,
			want:      true,
		},
		{
			name:      "connect",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Connect}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, allScopes),
			operation: admissionv1.Connect,
			resource:  appsDeployments,
			want:      true,
		},
		{
			name:      "operation wildcard",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, allScopes),
			operation: admissionv1.Delete,
			resource:  appsDeployments,
			want:      true,
		},
		{
			name:      "operation mismatch",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, allScopes),
			operation: admissionv1.Update,
			resource:  appsDeployments,
		},
		{
			name:      "group wildcard",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"*"}, []string{"v1"}, []string{"deployments"}, allScopes),
			operation: admissionv1.Create,
			resource:  appsDeployments,
			want:      true,
		},
		{
			name:      "group mismatch",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"batch"}, []string{"v1"}, []string{"deployments"}, allScopes),
			operation: admissionv1.Create,
			resource:  appsDeployments,
		},
		{
			name:      "version wildcard",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"apps"}, []string{"*"}, []string{"deployments"}, allScopes),
			operation: admissionv1.Create,
			resource:  appsDeployments,
			want:      true,
		},
		{
			name:      "version mismatch",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"apps"}, []string{"v2"}, []string{"deployments"}, allScopes),
			operation: admissionv1.Create,
			resource:  appsDeployments,
		},
		{
			name:      "main resource",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Update}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, allScopes),
			operation: admissionv1.Update,
			resource:  appsDeployments,
			want:      true,
		},
		{
			name:        "main resource excludes subresource",
			rule:        exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Update}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, allScopes),
			operation:   admissionv1.Update,
			resource:    appsDeployments,
			subresource: "status",
		},
		{
			name:        "exact subresource",
			rule:        exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Update}, []string{"apps"}, []string{"v1"}, []string{"deployments/status"}, allScopes),
			operation:   admissionv1.Update,
			resource:    appsDeployments,
			subresource: "status",
			want:        true,
		},
		{
			name:        "resource subresource wildcard",
			rule:        exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Update}, []string{"apps"}, []string{"v1"}, []string{"deployments/*"}, allScopes),
			operation:   admissionv1.Update,
			resource:    appsDeployments,
			subresource: "scale",
			want:        true,
		},
		{
			name:      "resource wildcard matches main resource",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"apps"}, []string{"v1"}, []string{"*"}, allScopes),
			operation: admissionv1.Create,
			resource:  appsDeployments,
			want:      true,
		},
		{
			name:        "resource and subresource wildcard",
			rule:        exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Update}, []string{"apps"}, []string{"v1"}, []string{"*/*"}, allScopes),
			operation:   admissionv1.Update,
			resource:    appsDeployments,
			subresource: "status",
			want:        true,
		},
		{
			name:       "namespaced scope",
			rule:       exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, namespacedScope),
			operation:  admissionv1.Create,
			resource:   appsDeployments,
			namespaced: true,
			want:       true,
		},
		{
			name:      "namespaced scope excludes cluster request",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, namespacedScope),
			operation: admissionv1.Create,
			resource:  appsDeployments,
		},
		{
			name:      "cluster scope",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, clusterScope),
			operation: admissionv1.Create,
			resource:  appsDeployments,
			want:      true,
		},
		{
			name:       "cluster scope excludes namespaced request",
			rule:       exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, clusterScope),
			operation:  admissionv1.Create,
			resource:   appsDeployments,
			namespaced: true,
		},
		{
			name:       "namespace resource remains cluster scoped",
			rule:       exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{""}, []string{"v1"}, []string{"namespaces"}, clusterScope),
			operation:  admissionv1.Create,
			resource:   coreNamespaces,
			namespaced: true,
			want:       true,
		},
		{
			name:       "all scopes",
			rule:       exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, allScopes),
			operation:  admissionv1.Create,
			resource:   appsDeployments,
			namespaced: true,
			want:       true,
		},
		{
			name:      "nil scope follows all scopes",
			rule:      exactRule([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}, []string{"apps"}, []string{"v1"}, []string{"deployments"}, nil),
			operation: admissionv1.Create,
			resource:  appsDeployments,
			want:      true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := kube136.ExactRuleMatches(test.rule, test.operation, test.resource, test.subresource, test.namespaced)
			if got != test.want {
				t.Errorf("ExactRuleMatches() = %t, want %t", got, test.want)
			}
		})
	}
}

func exactRule(
	operations []admissionregistrationv1.OperationType,
	apiGroups []string,
	apiVersions []string,
	resources []string,
	ruleScope *admissionregistrationv1.ScopeType,
) admissionregistrationv1.RuleWithOperations {
	return admissionregistrationv1.RuleWithOperations{
		Operations: operations,
		Rule: admissionregistrationv1.Rule{
			APIGroups:   apiGroups,
			APIVersions: apiVersions,
			Resources:   resources,
			Scope:       ruleScope,
		},
	}
}

func scope(value admissionregistrationv1.ScopeType) *admissionregistrationv1.ScopeType {
	return &value
}
