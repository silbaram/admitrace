package celauthorizer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/silbaram/admitrace/conformance/parity"
	"github.com/silbaram/admitrace/internal/contract"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	celplugin "k8s.io/apiserver/pkg/admission/plugin/cel"
	officialmatcher "k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
	"k8s.io/apiserver/pkg/authentication/user"
	apiservercel "k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/environment"
)

func TestOfficialMatcherDifferential(t *testing.T) {
	for _, name := range []string{
		"cel-compile-fail",
		"cel-cost-fail",
		"authorizer-allow",
		"authorizer-deny",
		"authorizer-no-opinion",
		"authorizer-error",
	} {
		t.Run(name, func(t *testing.T) {
			testCase := findDifferentialCase(t, name)
			if testCase.OracleType != parity.OracleOfficialMatcherDifferential {
				t.Fatalf("Scenario oracleType = %q, want %q", testCase.OracleType, parity.OracleOfficialMatcherDifferential)
			}
			product, err := evaluate(testCase)
			if err != nil {
				t.Fatal(err)
			}
			if err := parity.CompareEvaluation(testCase, product); err != nil {
				t.Fatal(err)
			}
			oracleOutcome, err := officialMatchConditionOutcome(t.Context(), testCase)
			if err != nil {
				t.Fatal(err)
			}
			if product.Outcome == nil {
				t.Fatalf("official matcher differential outcome: got <nil>, want %q", oracleOutcome)
			}
			if got := *product.Outcome; got != oracleOutcome {
				t.Fatalf("official matcher differential outcome: got %q, want %q", got, oracleOutcome)
			}
		})
	}
}

func officialMatchConditionOutcome(ctx context.Context, testCase parity.Case) (contract.Outcome, error) {
	snapshot, err := parity.BuildSnapshot(testCase)
	if err != nil {
		return "", err
	}
	attributes, err := officialVersionedAttributes(testCase.Scenario)
	if err != nil {
		return "", err
	}
	webhook := testCase.Scenario.Configuration.Validating.Webhooks[0]
	filter := officialConditionFilter(webhook.MatchConditions)
	switch testCase.Scenario.Metadata.Name {
	case "cel-cost-fail":
		filter = &errorConditionFilter{
			condition: &officialmatcher.MatchCondition{Name: "cost", Expression: "true"},
			err:       apiservercel.ErrOutOfBudget,
		}
	case "authorizer-error":
		filter = &errorConditionFilter{
			condition: &officialmatcher.MatchCondition{Name: "authorization", Expression: authorizationExpression()},
			err:       errors.New("fixture authorizer error"),
		}
	}
	matcher := officialmatcher.NewMatcher(filter, webhook.FailurePolicy, "webhook", "differential", testCase.Scenario.Metadata.Name)
	result := matcher.Match(ctx, attributes, nil, snapshot.Authorizer)
	switch {
	case result.Error != nil:
		return contract.OutcomeRejectedBeforeCall, nil
	case result.Matches:
		return contract.OutcomeCalled, nil
	default:
		return contract.OutcomeSkipped, nil
	}
}

func findDifferentialCase(t *testing.T, name string) parity.Case {
	t.Helper()
	for _, testCase := range cases() {
		if testCase.Scenario.Metadata.Name == name {
			return testCase
		}
	}
	t.Fatalf("Scenario %q not found", name)
	return parity.Case{}
}

func officialConditionFilter(conditions []admissionregistrationv1.MatchCondition) celplugin.ConditionEvaluator {
	accessors := make([]celplugin.ExpressionAccessor, len(conditions))
	for index := range conditions {
		condition := conditions[index]
		accessors[index] = &officialmatcher.MatchCondition{Name: condition.Name, Expression: condition.Expression}
	}
	compiler := celplugin.NewConditionCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
	return compiler.CompileCondition(
		accessors,
		celplugin.OptionalVariableDeclarations{HasAuthorizer: true},
		environment.StoredExpressions,
	)
}

func officialVersionedAttributes(input contract.Scenario) (*admission.VersionedAttributes, error) {
	var object map[string]any
	if err := json.Unmarshal(input.Request.Object, &object); err != nil {
		return nil, fmt.Errorf("decode request object: %w", err)
	}
	versionedObject := &unstructured.Unstructured{Object: object}
	kind := schema.GroupVersionKind(input.Request.Kind)
	versionedObject.SetGroupVersionKind(kind)
	requestUser := &user.DefaultInfo{
		Name: input.Request.UserInfo.Username, UID: input.Request.UserInfo.UID,
		Groups: append([]string(nil), input.Request.UserInfo.Groups...),
	}
	dryRun := input.Request.DryRun != nil && *input.Request.DryRun
	attributes := admission.NewAttributesRecord(
		versionedObject, nil, kind, input.Request.Namespace, input.Request.Name,
		schema.GroupVersionResource(input.Request.Resource), input.Request.SubResource,
		admission.Operation(input.Request.Operation), nil, dryRun, requestUser,
	)
	return &admission.VersionedAttributes{
		Attributes: attributes, VersionedObject: versionedObject, VersionedKind: kind,
	}, nil
}

type errorConditionFilter struct {
	condition *officialmatcher.MatchCondition
	err       error
}

func (f *errorConditionFilter) ForInput(
	context.Context,
	*admission.VersionedAttributes,
	*admissionv1.AdmissionRequest,
	celplugin.OptionalVariableBindings,
	*corev1.Namespace,
	int64,
) ([]celplugin.EvaluationResult, int64, error) {
	return []celplugin.EvaluationResult{{
		ExpressionAccessor: f.condition,
		Error:              f.err,
	}}, 0, nil
}

func (f *errorConditionFilter) CompilationErrors() []error {
	return nil
}

var _ celplugin.ConditionEvaluator = (*errorConditionFilter)(nil)
