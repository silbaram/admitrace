package celauthorizer

import (
	"context"
	"slices"
	"testing"

	"github.com/silbaram/admitrace/conformance/parity"
	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/matchcondition"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const groupName = "cel-authorizer"

func TestCELAuthorizerParity(t *testing.T) {
	testCases := cases()
	if err := parity.ValidateCases(groupName, testCases); err != nil {
		t.Fatal(err)
	}
	for _, row := range parity.CoverageReport(testCases) {
		t.Log(row)
	}

	for _, testCase := range testCases {
		t.Run(testCase.Scenario.Metadata.Name, func(t *testing.T) {
			actual, err := evaluate(testCase)
			if err != nil {
				t.Fatal(err)
			}
			if err := parity.CompareEvaluation(testCase, actual); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCELAuthorizerCoverage(t *testing.T) {
	testCases := cases()
	if got, want := len(testCases), 13; got != want {
		t.Fatalf("Scenario count = %d, want %d", got, want)
	}
	wantTags := []string{
		"authorizer-allow",
		"authorizer-deny",
		"authorizer-error",
		"authorizer-missing",
		"authorizer-no-opinion",
		"cel-compile-error",
		"cel-cost-error",
		"cel-runtime-error",
		"failure-policy-fail",
		"failure-policy-ignore",
		"false-and-error",
		"match-condition-none",
		"match-condition-true",
	}
	var gotTags []string
	for _, testCase := range testCases {
		gotTags = append(gotTags, testCase.CoverageTags...)
	}
	for _, wantTag := range wantTags {
		if !slices.Contains(gotTags, wantTag) {
			t.Errorf("coverage tags missing %q", wantTag)
		}
	}
}

func evaluate(testCase parity.Case) (contract.WebhookEvaluation, error) {
	if testCase.Scenario.Metadata.Name != "cel-cost-fail" {
		return parity.Evaluate(context.Background(), testCase)
	}
	snapshot, err := parity.BuildSnapshot(testCase)
	if err != nil {
		return contract.WebhookEvaluation{}, err
	}
	webhook := snapshot.Webhooks[0]
	result := matchcondition.NewEvaluator().Evaluate(
		context.Background(),
		webhook,
		snapshot.Request,
		matchcondition.Invocation{
			Resource: snapshot.Request.Resource,
			Kind:     snapshot.Request.Kind,
		},
		snapshot.Authorizer,
		matchcondition.WithCostBudgetForTest(0),
	)
	return contract.WebhookEvaluation{
		ConfigurationKind: webhook.ConfigurationKind,
		WebhookName:       webhook.Name,
		WebhookIndex:      webhook.InputIndex,
		SourcePath:        webhook.SourcePath,
		Determination:     result.Determination,
		Outcome:           result.Outcome,
		Trace:             result.Trace,
	}, nil
}

func cases() []parity.Case {
	called := contract.OutcomeCalled
	skipped := contract.OutcomeSkipped
	rejected := contract.OutcomeRejectedBeforeCall
	return []parity.Case{
		{
			Group: groupName, Scenario: celScenario("cel-no-conditions", nil, admissionregistrationv1.Fail, nil),
			OracleType: parity.OracleKubeAPIServerObservation, OracleRationale: "an empty matchConditions list is directly observable as an API server webhook call",
			CoverageTags: []string{"kube-apiserver", "match-condition-none", "validating"},
			Expected:     parity.ExpectedResult{Determination: contract.DeterminationDeterminate, Outcome: &called, ReasonCode: contract.ReasonCodeMatchConditionsTrue, Diagnostics: []contract.ReasonCode{contract.ReasonCodeCapabilityOutsideProfile}},
		},
		{
			Group: groupName, Scenario: celScenario("cel-true", conditions(condition("true", "true")), admissionregistrationv1.Fail, nil),
			OracleType: parity.OracleKubeAPIServerObservation, OracleRationale: "a literal true condition is directly observable as an API server webhook call",
			CoverageTags: []string{"kube-apiserver", "match-condition-true", "validating"},
			Expected:     parity.ExpectedResult{Determination: contract.DeterminationDeterminate, Outcome: &called, ReasonCode: contract.ReasonCodeMatchConditionsTrue, Diagnostics: []contract.ReasonCode{contract.ReasonCodeCapabilityOutsideProfile}},
		},
		{
			Group: groupName, Scenario: celScenario("cel-false", conditions(condition("false", "false")), admissionregistrationv1.Fail, nil),
			OracleType: parity.OracleKubeAPIServerObservation, OracleRationale: "a literal false condition is directly observable as an API server skip",
			CoverageTags: []string{"kube-apiserver", "match-condition-false", "validating"},
			Expected:     parity.ExpectedResult{Determination: contract.DeterminationDeterminate, Outcome: &skipped, ReasonCode: contract.ReasonCodeMatchConditionFalse},
		},
		{
			Group: groupName, Scenario: celScenario("cel-false-overrides-error", conditions(condition("error", "1 / 0 == 0"), condition("false", "false")), admissionregistrationv1.Fail, nil),
			OracleType: parity.OracleGoldenTrace, OracleRationale: "the reviewed Kubernetes reducer trace preserves every observation while false controls the result",
			CoverageTags: []string{"false-and-error", "golden-trace", "match-condition-false"},
			Expected: parity.ExpectedResult{Determination: contract.DeterminationDeterminate, Outcome: &skipped, ReasonCode: contract.ReasonCodeMatchConditionFalse, Trace: append(preCELTrace(),
				trace("matchConditions", contract.TraceResultError, contract.ReasonCodeCELRuntimeError, false),
				trace("matchConditions", contract.TraceResultFalse, contract.ReasonCodeMatchConditionFalse, false),
				trace("matchConditions", contract.TraceResultFalse, contract.ReasonCodeMatchConditionFalse, true),
			)},
		},
		{
			Group: groupName, Scenario: celScenario("cel-runtime-fail", conditions(condition("runtime", "1 / 0 == 0")), admissionregistrationv1.Fail, nil),
			OracleType: parity.OracleKubeAPIServerObservation, OracleRationale: "failurePolicy Fail converts the runtime error into an API server rejection before transport",
			CoverageTags: []string{"cel-runtime-error", "failure-policy-fail", "kube-apiserver"},
			Expected:     parity.ExpectedResult{Determination: contract.DeterminationDeterminate, Outcome: &rejected, ReasonCode: contract.ReasonCodeCELRuntimeError, Diagnostics: []contract.ReasonCode{contract.ReasonCodeCELRuntimeError}},
		},
		{
			Group: groupName, Scenario: celScenario("cel-runtime-ignore", conditions(condition("runtime", "1 / 0 == 0")), admissionregistrationv1.Ignore, nil),
			OracleType: parity.OracleGoldenTrace, OracleRationale: "the Kubernetes reducer golden records failurePolicy Ignore turning the same error into a skip",
			CoverageTags: []string{"cel-runtime-error", "failure-policy-ignore", "golden-trace"},
			Expected: parity.ExpectedResult{Determination: contract.DeterminationDeterminate, Outcome: &skipped, ReasonCode: contract.ReasonCodeCELRuntimeError, Trace: append(preCELTrace(),
				trace("matchConditions", contract.TraceResultError, contract.ReasonCodeCELRuntimeError, false),
				trace("matchConditions", contract.TraceResultError, contract.ReasonCodeCELRuntimeError, true),
			), Diagnostics: []contract.ReasonCode{contract.ReasonCodeCELRuntimeError}},
		},
		{
			Group: groupName, Scenario: celScenario("cel-compile-fail", conditions(condition("compile", "unknownVariable == true")), admissionregistrationv1.Fail, nil),
			OracleType: parity.OracleGoldenTrace, OracleRationale: "the stored-expression compiler category is stable and does not require a live authorization decision",
			CoverageTags: []string{"cel-compile-error", "failure-policy-fail", "golden-trace"},
			Expected: parity.ExpectedResult{Determination: contract.DeterminationDeterminate, Outcome: &rejected, ReasonCode: contract.ReasonCodeCELCompileError, Trace: append(preCELTrace(),
				trace("matchConditions", contract.TraceResultError, contract.ReasonCodeCELCompileError, false),
				trace("matchConditions", contract.TraceResultError, contract.ReasonCodeCELCompileError, true),
			), Diagnostics: []contract.ReasonCode{contract.ReasonCodeCELCompileError}},
		},
		{
			Group: groupName, Scenario: celScenario("cel-cost-fail", conditions(condition("cost", "request.resource.group == 'apps'")), admissionregistrationv1.Fail, nil),
			OracleType: parity.OracleGoldenTrace, OracleRationale: "a zero test budget deterministically exercises the Kubernetes runtime cost category without oversized input",
			CoverageTags: []string{"cel-cost-error", "failure-policy-fail", "golden-trace"},
			Expected: parity.ExpectedResult{Determination: contract.DeterminationDeterminate, Outcome: &rejected, ReasonCode: contract.ReasonCodeCELCostBudgetExceeded, Trace: []parity.TraceExpectation{
				trace("matchConditions", contract.TraceResultError, contract.ReasonCodeCELCostBudgetExceeded, false),
				trace("matchConditions", contract.TraceResultError, contract.ReasonCodeCELCostBudgetExceeded, true),
			}},
		},
		authorizerCase("authorizer-allow", contract.AuthorizationVerdictAllow, parity.OracleGoldenTrace, "the exact allow fixture is a recorded authorization contract and yields true", []string{"authorizer-allow", "fixture-contract", "golden-trace"}, contract.DeterminationDeterminate, &called, contract.ReasonCodeMatchConditionsTrue),
		authorizerCase("authorizer-deny", contract.AuthorizationVerdictDeny, parity.OracleGoldenTrace, "the exact deny fixture is a recorded authorization contract and yields false", []string{"authorizer-deny", "fixture-contract", "golden-trace"}, contract.DeterminationDeterminate, &skipped, contract.ReasonCodeMatchConditionFalse),
		authorizerCase("authorizer-no-opinion", contract.AuthorizationVerdictNoOpinion, parity.OracleGoldenTrace, "the exact no-opinion fixture is intentionally false rather than an authorization transport error", []string{"authorizer-no-opinion", "fixture-contract", "golden-trace"}, contract.DeterminationDeterminate, &skipped, contract.ReasonCodeMatchConditionFalse),
		authorizerCase("authorizer-error", contract.AuthorizationVerdictError, parity.OracleGoldenTrace, "the explicit fixture error records the Kubernetes evaluation category without exposing authorization details", []string{"authorizer-error", "failure-policy-fail", "fixture-contract"}, contract.DeterminationDeterminate, &rejected, contract.ReasonCodeCELAuthorizationError),
		{
			Group: groupName, Scenario: celScenario("authorizer-missing", conditions(condition("authorization", authorizationExpression())), admissionregistrationv1.Fail, nil),
			OracleType: parity.OracleIncompleteContract, OracleRationale: "no live SubjectAccessReview is allowed, so an absent exact decision must remain incomplete",
			CoverageTags: []string{"authorizer-missing", "incomplete-contract", "missing-fixture"},
			Expected: parity.ExpectedResult{Determination: contract.DeterminationIndeterminate, ReasonCode: contract.ReasonCodeAuthorizationContextMissing, Trace: append(preCELTrace(),
				trace("matchConditions", contract.TraceResultIndeterminate, contract.ReasonCodeAuthorizationContextMissing, false),
				trace("matchConditions", contract.TraceResultIndeterminate, contract.ReasonCodeAuthorizationContextMissing, true),
			), Diagnostics: []contract.ReasonCode{contract.ReasonCodeAuthorizationContextMissing}},
		},
	}
}

func authorizerCase(name string, verdict contract.AuthorizationVerdict, oracleType parity.OracleType, rationale string, tags []string, determination contract.Determination, outcome *contract.Outcome, reason contract.ReasonCode) parity.Case {
	traceResult := contract.TraceResultTrue
	if reason == contract.ReasonCodeMatchConditionFalse {
		traceResult = contract.TraceResultFalse
	} else if reason == contract.ReasonCodeCELAuthorizationError {
		traceResult = contract.TraceResultError
	}
	diagnostics := []contract.ReasonCode(nil)
	if outcome != nil && *outcome == contract.OutcomeCalled {
		diagnostics = []contract.ReasonCode{contract.ReasonCodeCapabilityOutsideProfile}
	} else if reason == contract.ReasonCodeCELAuthorizationError {
		diagnostics = []contract.ReasonCode{contract.ReasonCodeCELAuthorizationError}
	}
	return parity.Case{
		Group: groupName, Scenario: celScenario(name, conditions(condition("authorization", authorizationExpression())), admissionregistrationv1.Fail, []contract.AuthorizationDecision{authorizationDecision(verdict)}),
		OracleType: oracleType, OracleRationale: rationale, CoverageTags: tags,
		Expected: parity.ExpectedResult{Determination: determination, Outcome: outcome, ReasonCode: reason, Trace: append(preCELTrace(),
			trace("matchConditions", traceResult, observationReason(reason), false),
			trace("matchConditions", traceResult, reason, true),
		), Diagnostics: diagnostics},
	}
}

func celScenario(name string, matchConditions []admissionregistrationv1.MatchCondition, failurePolicy admissionregistrationv1.FailurePolicyType, authorization []contract.AuthorizationDecision) contract.Scenario {
	exact := admissionregistrationv1.Exact
	scope := admissionregistrationv1.NamespacedScope
	input := contract.Scenario{
		APIVersion: contract.ScenarioAPIVersion, Kind: contract.ScenarioKind,
		Metadata: contract.ScenarioMetadata{Name: name}, CompatibilityProfile: contract.Kubernetes136DefaultProfile(),
		Configuration: contract.WebhookConfiguration{Validating: &kube136.ValidatingWebhookConfiguration{
			TypeMeta: metav1.TypeMeta{APIVersion: kube136.AdmissionRegistrationAPIVersion, Kind: string(contract.ConfigurationKindValidating)},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{{
				Name: "advanced.admitrace.io", FailurePolicy: &failurePolicy, MatchPolicy: &exact, MatchConditions: matchConditions,
				Rules: []admissionregistrationv1.RuleWithOperations{{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create}, Rule: admissionregistrationv1.Rule{
					APIGroups: []string{"apps"}, APIVersions: []string{"v1"}, Resources: []string{"deployments"}, Scope: &scope,
				}}},
			}},
		}},
		Request: contract.AdmissionRequest{
			Kind: metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, Resource: metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			Name: "demo", Namespace: "default", Operation: admissionv1.Create, Scope: contract.RequestScopeNamespaced,
			Object:   []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"demo","namespace":"default"}}`),
			UserInfo: authenticationv1.UserInfo{Username: "fixture-user", Groups: []string{"fixture-group"}},
		},
	}
	if authorization != nil {
		input.ExternalContext = &contract.ExternalContext{Authorization: authorization}
	}
	return input
}

func authorizationExpression() string {
	return "authorizer.group('apps').resource('deployments').namespace('default').name('demo').check('get').allowed()"
}

func authorizationDecision(verdict contract.AuthorizationVerdict) contract.AuthorizationDecision {
	return contract.AuthorizationDecision{Query: contract.AuthorizationQuery{Resource: &contract.ResourceAuthorizationQuery{
		Verb: "get", APIGroup: "apps", APIVersion: "*", Resource: "deployments", Namespace: "default", Name: "demo",
	}}, Verdict: verdict, Reason: "recorded fixture decision"}
}

func conditions(values ...admissionregistrationv1.MatchCondition) []admissionregistrationv1.MatchCondition {
	return values
}

func condition(name, expression string) admissionregistrationv1.MatchCondition {
	return admissionregistrationv1.MatchCondition{Name: name, Expression: expression}
}

func preCELTrace() []parity.TraceExpectation {
	return []parity.TraceExpectation{
		trace("namespaceSelector", contract.TraceResultMatch, contract.ReasonCodeNamespaceSelectorMatch, false),
		trace("objectSelector", contract.TraceResultMatch, contract.ReasonCodeObjectSelectorMatch, false),
		trace("exactRules", contract.TraceResultMatch, contract.ReasonCodeRuleMatch, false),
	}
}

func observationReason(summaryReason contract.ReasonCode) contract.ReasonCode {
	if summaryReason == contract.ReasonCodeMatchConditionsTrue {
		return contract.ReasonCodeMatchConditionTrue
	}
	return summaryReason
}

func trace(stage string, result contract.TraceResult, reason contract.ReasonCode, terminal bool) parity.TraceExpectation {
	return parity.TraceExpectation{Stage: stage, Result: result, ReasonCode: reason, Terminal: terminal}
}
