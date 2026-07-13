package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	"github.com/silbaram/admitrace/internal/scenario"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type coreParityAction string

const (
	createConfigMap            coreParityAction = "create-configmap"
	updateConfigMap            coreParityAction = "update-configmap"
	deleteConfigMap            coreParityAction = "delete-configmap"
	updateNamespaceStatus      coreParityAction = "update-namespace-status"
	createNamespace            coreParityAction = "create-namespace"
	createClusterRole          coreParityAction = "create-cluster-role"
	createWebhookConfiguration coreParityAction = "create-webhook-configuration"
)

type coreParityWebhook struct {
	name              string
	rules             []admissionregistrationv1.RuleWithOperations
	namespaceSelector *metav1.LabelSelector
	objectSelector    *metav1.LabelSelector
	want              contract.Outcome
}

type coreParityRequest struct {
	action       coreParityAction
	operation    admissionv1.Operation
	kind         metav1.GroupVersionKind
	resource     metav1.GroupVersionResource
	subresource  string
	scope        contract.RequestScope
	namespace    string
	name         string
	objectLabels map[string]string
	oldLabels    map[string]string
}

type coreParityCase struct {
	name            string
	kind            ConfigurationKind
	tags            []string
	request         coreParityRequest
	webhooks        []coreParityWebhook
	namespaceLabels map[string]string
}

func TestCoreParityMatrixContract(t *testing.T) {
	t.Parallel()

	cases := coreParityCases()
	if got, wantMinimum := len(cases), 10; got < wantMinimum {
		t.Fatalf("case count = %d, want at least %d", got, wantMinimum)
	}

	requiredCoverage := []string{
		"configuration:validating",
		"configuration:mutating",
		"operation:create",
		"operation:update",
		"operation:delete",
		"gvr:match",
		"gvr:no-match",
		"subresource:status",
		"scope:namespaced",
		"scope:cluster",
		"namespace-selector:namespace",
		"namespace-selector:namespaced",
		"namespace-selector:cluster",
		"object-selector:old-object-or",
		"object-selector:no-match",
		"rules:multiple",
		"webhooks:multiple",
		"configuration:self-exclusion",
	}
	coveredTags := make(map[string]bool, len(requiredCoverage))
	for _, tag := range requiredCoverage {
		coveredTags[tag] = false
	}
	seenNames := make(map[string]struct{}, len(cases))
	for _, testCase := range cases {
		if _, duplicate := seenNames[testCase.name]; duplicate {
			t.Errorf("case name = %q, want unique", testCase.name)
		}
		seenNames[testCase.name] = struct{}{}
		for _, tag := range testCase.tags {
			if _, tracked := coveredTags[tag]; tracked {
				coveredTags[tag] = true
			}
		}

		result, err := evaluateCoreParityCase(context.Background(), testCase)
		if err != nil {
			t.Errorf("case %q product evaluation error = %v", testCase.name, err)
			continue
		}
		if got, want := len(result.Webhooks), len(testCase.webhooks); got != want {
			t.Errorf("case %q Webhook count = %d, want %d", testCase.name, got, want)
			continue
		}
		for index, got := range result.Webhooks {
			want := testCase.webhooks[index].want
			if got.Determination != contract.DeterminationDeterminate {
				t.Errorf("case %q Webhook %q determination = %q, want %q", testCase.name, got.WebhookName, got.Determination, contract.DeterminationDeterminate)
				continue
			}
			if got.Outcome == nil || *got.Outcome != want {
				t.Errorf("case %q Webhook %q outcome = %s, want %q", testCase.name, got.WebhookName, outcomeText(got.Outcome), want)
			}
		}
	}

	for _, tag := range requiredCoverage {
		if !coveredTags[tag] {
			t.Errorf("coverage tag %q = absent, want present", tag)
		}
	}
}

func TestCoreParityMismatchIncludesBothTerminals(t *testing.T) {
	t.Parallel()

	testCase := coreParityCases()[0]
	result, err := evaluateCoreParityCase(context.Background(), testCase)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := &coreParityMismatch{
		caseName:    testCase.name,
		webhookName: testCase.webhooks[0].name,
		product:     result.Webhooks[0],
		oracle:      Observation{Kind: ObservationSkipped},
	}
	message := mismatch.Error()
	for _, want := range []string{"got oracle={", "want product={", "terminal trace difference", "matchConditions", "skipped"} {
		if !strings.Contains(message, want) {
			t.Errorf("Error() = %q, want substring %q", message, want)
		}
	}
}

func coreParityCases() []coreParityCase {
	namespaced := admissionregistrationv1.NamespacedScope
	cluster := admissionregistrationv1.ClusterScope
	labelsMatch := &metav1.LabelSelector{MatchLabels: map[string]string{"route": "yes"}}
	namespaceMatch := &metav1.LabelSelector{MatchLabels: map[string]string{"environment": "parity"}}
	return []coreParityCase{
		{
			name: "validating-create-called", kind: ValidatingConfiguration,
			tags:     []string{"configuration:validating", "operation:create", "gvr:match", "subresource:root", "scope:namespaced"},
			request:  configMapRequest(createConfigMap, admissionv1.Create, "core-create", map[string]string{"route": "yes"}, nil),
			webhooks: []coreParityWebhook{{name: "create.validating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Create, "", "v1", "configmaps", namespaced)}, want: contract.OutcomeCalled}},
		},
		{
			name: "mutating-update-called", kind: MutatingConfiguration,
			tags:     []string{"configuration:mutating", "operation:update", "gvr:match", "subresource:root", "scope:namespaced"},
			request:  configMapRequest(updateConfigMap, admissionv1.Update, "core-update", map[string]string{"route": "new"}, map[string]string{"route": "old"}),
			webhooks: []coreParityWebhook{{name: "update.mutating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Update, "", "v1", "configmaps", namespaced)}, want: contract.OutcomeCalled}},
		},
		{
			name: "validating-delete-operation-no-match", kind: ValidatingConfiguration,
			tags:     []string{"configuration:validating", "operation:delete", "gvr:match", "subresource:root", "scope:namespaced"},
			request:  configMapRequest(deleteConfigMap, admissionv1.Delete, "core-delete", nil, map[string]string{"route": "yes"}),
			webhooks: []coreParityWebhook{{name: "delete.validating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Create, "", "v1", "configmaps", namespaced)}, want: contract.OutcomeSkipped}},
		},
		{
			name: "mutating-gvr-no-match", kind: MutatingConfiguration,
			tags:     []string{"configuration:mutating", "operation:create", "gvr:no-match", "subresource:root", "scope:namespaced"},
			request:  configMapRequest(createConfigMap, admissionv1.Create, "core-gvr-miss", nil, nil),
			webhooks: []coreParityWebhook{{name: "gvr.mutating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Create, "", "v1", "secrets", namespaced)}, want: contract.OutcomeSkipped}},
		},
		{
			name: "validating-namespace-status-subresource", kind: ValidatingConfiguration,
			tags:     []string{"configuration:validating", "operation:update", "gvr:match", "subresource:status", "scope:cluster"},
			request:  namespaceStatusRequest("core-status"),
			webhooks: []coreParityWebhook{{name: "status.validating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Update, "", "v1", "namespaces/status", cluster)}, want: contract.OutcomeCalled}},
		},
		{
			name: "mutating-scope-no-match", kind: MutatingConfiguration,
			tags:     []string{"configuration:mutating", "operation:create", "gvr:match", "subresource:root", "scope:namespaced"},
			request:  configMapRequest(createConfigMap, admissionv1.Create, "core-scope-miss", nil, nil),
			webhooks: []coreParityWebhook{{name: "scope.mutating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Create, "", "v1", "configmaps", cluster)}, want: contract.OutcomeSkipped}},
		},
		{
			name: "validating-namespace-selector-namespace-object", kind: ValidatingConfiguration,
			tags:     []string{"configuration:validating", "operation:create", "gvr:match", "subresource:root", "scope:cluster", "namespace-selector:namespace"},
			request:  namespaceCreateRequest("core-namespace-selector", map[string]string{"environment": "parity"}),
			webhooks: []coreParityWebhook{{name: "namespace.validating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Create, "", "v1", "namespaces", cluster)}, namespaceSelector: namespaceMatch, want: contract.OutcomeCalled}},
		},
		{
			name: "mutating-namespace-selector-namespaced", kind: MutatingConfiguration,
			tags:            []string{"configuration:mutating", "operation:create", "gvr:match", "subresource:root", "scope:namespaced", "namespace-selector:namespaced"},
			request:         configMapRequest(createConfigMap, admissionv1.Create, "core-namespaced-selector", nil, nil),
			namespaceLabels: map[string]string{"environment": "parity"},
			webhooks:        []coreParityWebhook{{name: "namespaced.mutating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Create, "", "v1", "configmaps", namespaced)}, namespaceSelector: namespaceMatch, want: contract.OutcomeCalled}},
		},
		{
			name: "validating-namespace-selector-cluster-bypass", kind: ValidatingConfiguration,
			tags:     []string{"configuration:validating", "operation:create", "gvr:match", "subresource:root", "scope:cluster", "namespace-selector:cluster"},
			request:  clusterRoleRequest("core-cluster-selector"),
			webhooks: []coreParityWebhook{{name: "cluster.validating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Create, "rbac.authorization.k8s.io", "v1", "clusterroles", cluster)}, namespaceSelector: namespaceMatch, want: contract.OutcomeCalled}},
		},
		{
			name: "mutating-object-selector-old-object-or", kind: MutatingConfiguration,
			tags:     []string{"configuration:mutating", "operation:update", "gvr:match", "subresource:root", "scope:namespaced", "object-selector:old-object-or"},
			request:  configMapRequest(updateConfigMap, admissionv1.Update, "core-old-object", map[string]string{"route": "no"}, map[string]string{"route": "yes"}),
			webhooks: []coreParityWebhook{{name: "old-object.mutating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Update, "", "v1", "configmaps", namespaced)}, objectSelector: labelsMatch, want: contract.OutcomeCalled}},
		},
		{
			name: "validating-object-selector-no-match", kind: ValidatingConfiguration,
			tags:     []string{"configuration:validating", "operation:create", "gvr:match", "subresource:root", "scope:namespaced", "object-selector:no-match"},
			request:  configMapRequest(createConfigMap, admissionv1.Create, "core-object-miss", map[string]string{"route": "no"}, nil),
			webhooks: []coreParityWebhook{{name: "object.validating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Create, "", "v1", "configmaps", namespaced)}, objectSelector: labelsMatch, want: contract.OutcomeSkipped}},
		},
		{
			name: "mutating-multiple-rules-and-webhooks", kind: MutatingConfiguration,
			tags:    []string{"configuration:mutating", "operation:create", "gvr:match", "subresource:root", "scope:namespaced", "rules:multiple", "webhooks:multiple"},
			request: configMapRequest(createConfigMap, admissionv1.Create, "core-multiple", map[string]string{"route": "yes"}, nil),
			webhooks: []coreParityWebhook{
				{name: "multiple-first.mutating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Create, "", "v1", "secrets", namespaced), rule(admissionv1.Create, "", "v1", "configmaps", namespaced)}, want: contract.OutcomeCalled},
				{name: "multiple-second.mutating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Create, "", "v1", "configmaps", namespaced)}, objectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"route": "other"}}, want: contract.OutcomeSkipped},
			},
		},
		{
			name: "validating-webhook-configuration-self-exclusion", kind: ValidatingConfiguration,
			tags:     []string{"configuration:validating", "operation:create", "gvr:match", "subresource:root", "scope:cluster", "configuration:self-exclusion"},
			request:  webhookConfigurationRequest("core-self-exclusion"),
			webhooks: []coreParityWebhook{{name: "self.validating.core.admitrace.io", rules: []admissionregistrationv1.RuleWithOperations{rule(admissionv1.Create, "admissionregistration.k8s.io", "v1", "validatingwebhookconfigurations", cluster)}, want: contract.OutcomeSkipped}},
		},
	}
}

func evaluateCoreParityCase(ctx context.Context, testCase coreParityCase) (contract.EvaluationResult, error) {
	object, err := rawObject(testCase.request, false)
	if err != nil {
		return contract.EvaluationResult{}, fmt.Errorf("marshal product request object for %q: %w", testCase.name, err)
	}
	oldObject, err := rawObject(testCase.request, true)
	if err != nil {
		return contract.EvaluationResult{}, fmt.Errorf("marshal product oldObject for %q: %w", testCase.name, err)
	}
	input := contract.Scenario{
		APIVersion:           contract.ScenarioAPIVersion,
		Kind:                 contract.ScenarioKind,
		Metadata:             contract.ScenarioMetadata{Name: testCase.name},
		CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		Request: contract.AdmissionRequest{
			Kind:        testCase.request.kind,
			Resource:    testCase.request.resource,
			SubResource: testCase.request.subresource,
			Name:        testCase.request.name,
			Namespace:   testCase.request.namespace,
			Operation:   testCase.request.operation,
			Scope:       testCase.request.scope,
			Object:      object,
			OldObject:   oldObject,
		},
	}
	if testCase.namespaceLabels != nil {
		input.ExternalContext = &contract.ExternalContext{Namespace: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: testCase.request.namespace, Labels: cloneLabels(testCase.namespaceLabels)},
		}}
	}
	input.Configuration = productConfiguration(testCase)
	if err := scenario.Validate(&input); err != nil {
		return contract.EvaluationResult{}, fmt.Errorf("validate product Scenario %q: %w", testCase.name, err)
	}
	scenario.ApplyDefaults(&input)
	snapshot, err := evaluation.SnapshotFromScenario(input)
	if err != nil {
		return contract.EvaluationResult{}, fmt.Errorf("build product snapshot %q: %w", testCase.name, err)
	}
	result := evaluation.NewEvaluator().Evaluate(ctx, snapshot)
	if err := result.Validate(); err != nil {
		return contract.EvaluationResult{}, fmt.Errorf("validate product result %q: %w", testCase.name, err)
	}
	return result, nil
}

func productConfiguration(testCase coreParityCase) contract.WebhookConfiguration {
	failurePolicy := admissionregistrationv1.Fail
	matchPolicy := admissionregistrationv1.Exact
	sideEffects := admissionregistrationv1.SideEffectClassNone
	if testCase.kind == ValidatingConfiguration {
		webhooks := make([]admissionregistrationv1.ValidatingWebhook, len(testCase.webhooks))
		for index, webhook := range testCase.webhooks {
			webhooks[index] = admissionregistrationv1.ValidatingWebhook{
				Name: webhook.name, Rules: webhook.rules, FailurePolicy: &failurePolicy,
				MatchPolicy: &matchPolicy, NamespaceSelector: webhook.namespaceSelector,
				ObjectSelector: webhook.objectSelector, SideEffects: &sideEffects,
				AdmissionReviewVersions: []string{"v1"},
			}
		}
		return contract.WebhookConfiguration{Validating: &kube136.ValidatingWebhookConfiguration{
			TypeMeta:   metav1.TypeMeta{APIVersion: kube136.AdmissionRegistrationAPIVersion, Kind: string(contract.ConfigurationKindValidating)},
			ObjectMeta: metav1.ObjectMeta{Name: "product-" + testCase.name}, Webhooks: webhooks,
		}}
	}

	webhooks := make([]admissionregistrationv1.MutatingWebhook, len(testCase.webhooks))
	for index, webhook := range testCase.webhooks {
		webhooks[index] = admissionregistrationv1.MutatingWebhook{
			Name: webhook.name, Rules: webhook.rules, FailurePolicy: &failurePolicy,
			MatchPolicy: &matchPolicy, NamespaceSelector: webhook.namespaceSelector,
			ObjectSelector: webhook.objectSelector, SideEffects: &sideEffects,
			AdmissionReviewVersions: []string{"v1"},
		}
	}
	return contract.WebhookConfiguration{Mutating: &kube136.MutatingWebhookConfiguration{
		TypeMeta:   metav1.TypeMeta{APIVersion: kube136.AdmissionRegistrationAPIVersion, Kind: string(contract.ConfigurationKindMutating)},
		ObjectMeta: metav1.ObjectMeta{Name: "product-" + testCase.name}, Webhooks: webhooks,
	}}
}

func rule(operation admissionv1.Operation, group, version, resource string, scope admissionregistrationv1.ScopeType) admissionregistrationv1.RuleWithOperations {
	return admissionregistrationv1.RuleWithOperations{
		Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationType(operation)},
		Rule: admissionregistrationv1.Rule{
			APIGroups: []string{group}, APIVersions: []string{version}, Resources: []string{resource}, Scope: &scope,
		},
	}
}

func configMapRequest(action coreParityAction, operation admissionv1.Operation, name string, objectLabels, oldLabels map[string]string) coreParityRequest {
	return coreParityRequest{
		action: action, operation: operation,
		kind:     metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		resource: metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
		scope:    contract.RequestScopeNamespaced, namespace: "core-" + stableSuffix(name), name: name,
		objectLabels: objectLabels, oldLabels: oldLabels,
	}
}

func namespaceCreateRequest(name string, labels map[string]string) coreParityRequest {
	return coreParityRequest{
		action: createNamespace, operation: admissionv1.Create,
		kind:     metav1.GroupVersionKind{Version: "v1", Kind: "Namespace"},
		resource: metav1.GroupVersionResource{Version: "v1", Resource: "namespaces"},
		scope:    contract.RequestScopeCluster, name: name, objectLabels: labels,
	}
}

func namespaceStatusRequest(name string) coreParityRequest {
	return coreParityRequest{
		action: updateNamespaceStatus, operation: admissionv1.Update,
		kind:        metav1.GroupVersionKind{Version: "v1", Kind: "Namespace"},
		resource:    metav1.GroupVersionResource{Version: "v1", Resource: "namespaces"},
		subresource: "status", scope: contract.RequestScopeCluster, name: name,
		objectLabels: map[string]string{"environment": "parity"}, oldLabels: map[string]string{"environment": "parity"},
	}
}

func clusterRoleRequest(name string) coreParityRequest {
	return coreParityRequest{
		action: createClusterRole, operation: admissionv1.Create,
		kind:     metav1.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		resource: metav1.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"},
		scope:    contract.RequestScopeCluster, name: name,
	}
}

func webhookConfigurationRequest(name string) coreParityRequest {
	return coreParityRequest{
		action: createWebhookConfiguration, operation: admissionv1.Create,
		kind:     metav1.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingWebhookConfiguration"},
		resource: metav1.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations"},
		scope:    contract.RequestScopeCluster, name: name,
	}
}

func rawObject(request coreParityRequest, old bool) (json.RawMessage, error) {
	labels := request.objectLabels
	if old {
		labels = request.oldLabels
	}
	if labels == nil && ((old && request.operation == admissionv1.Create) || (!old && request.operation == admissionv1.Delete)) {
		return nil, nil
	}
	apiVersion := request.kind.Version
	if request.kind.Group != "" {
		apiVersion = request.kind.Group + "/" + request.kind.Version
	}
	document := map[string]any{
		"apiVersion": apiVersion,
		"kind":       request.kind.Kind,
		"metadata": map[string]any{
			"name": request.name, "namespace": request.namespace, "labels": cloneLabels(labels),
		},
	}
	data, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func cloneLabels(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

type coreParityMismatch struct {
	caseName    string
	webhookName string
	product     contract.WebhookEvaluation
	oracle      Observation
}

func (e *coreParityMismatch) Error() string {
	stage, result, reason := terminalTrace(e.product.Trace)
	return fmt.Sprintf(
		"core parity mismatch case=%q webhook=%q: got oracle={observation:%s calls:%d terminal:%s}; want product={determination:%s outcome:%s terminal:%s/%s/%s}; terminal trace difference got=%s want=%s/%s/%s",
		e.caseName, e.webhookName, e.oracle.Kind, e.oracle.CallCount, e.oracle.Kind,
		e.product.Determination, outcomeText(e.product.Outcome), stage, result, reason,
		e.oracle.Kind, stage, result, reason,
	)
}

func terminalTrace(trace []contract.TraceStep) (string, contract.TraceResult, contract.ReasonCode) {
	for _, step := range trace {
		if step.Terminal {
			return step.Stage, step.Result, step.ReasonCode
		}
	}
	return "missing", "", ""
}

func outcomeText(outcome *contract.Outcome) string {
	if outcome == nil {
		return "absent"
	}
	return string(*outcome)
}
