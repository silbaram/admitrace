package matchcondition_test

import (
	"context"
	"errors"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	"github.com/silbaram/admitrace/internal/matchcondition"
	"github.com/silbaram/admitrace/internal/normalize"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestEvaluateMatchConditionsOutcomesAndPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		conditions    []admissionregistrationv1.MatchCondition
		failurePolicy admissionregistrationv1.FailurePolicyType
		wantOutcome   contract.Outcome
		wantReason    contract.ReasonCode
		wantTrace     int
		wantError     bool
	}{
		{
			name:          "no conditions",
			failurePolicy: admissionregistrationv1.Fail,
			wantOutcome:   contract.OutcomeCalled,
			wantReason:    contract.ReasonCodeMatchConditionsTrue,
			wantTrace:     1,
		},
		{
			name: "all true",
			conditions: []admissionregistrationv1.MatchCondition{
				{Name: "first", Expression: "true"},
				{Name: "request", Expression: "request.resource.group == 'apps'"},
			},
			failurePolicy: admissionregistrationv1.Fail,
			wantOutcome:   contract.OutcomeCalled,
			wantReason:    contract.ReasonCodeMatchConditionsTrue,
			wantTrace:     3,
		},
		{
			name: "one false",
			conditions: []admissionregistrationv1.MatchCondition{
				{Name: "false", Expression: "false"},
			},
			failurePolicy: admissionregistrationv1.Fail,
			wantOutcome:   contract.OutcomeSkipped,
			wantReason:    contract.ReasonCodeMatchConditionFalse,
			wantTrace:     2,
		},
		{
			name: "false overrides runtime error",
			conditions: []admissionregistrationv1.MatchCondition{
				{Name: "error", Expression: "1 / 0 == 0"},
				{Name: "false", Expression: "false"},
			},
			failurePolicy: admissionregistrationv1.Fail,
			wantOutcome:   contract.OutcomeSkipped,
			wantReason:    contract.ReasonCodeMatchConditionFalse,
			wantTrace:     3,
		},
		{
			name: "runtime error with Fail",
			conditions: []admissionregistrationv1.MatchCondition{
				{Name: "error", Expression: "1 / 0 == 0"},
			},
			failurePolicy: admissionregistrationv1.Fail,
			wantOutcome:   contract.OutcomeRejectedBeforeCall,
			wantReason:    contract.ReasonCodeCELRuntimeError,
			wantTrace:     2,
			wantError:     true,
		},
		{
			name: "runtime error with Ignore",
			conditions: []admissionregistrationv1.MatchCondition{
				{Name: "error", Expression: "1 / 0 == 0"},
			},
			failurePolicy: admissionregistrationv1.Ignore,
			wantOutcome:   contract.OutcomeSkipped,
			wantReason:    contract.ReasonCodeCELRuntimeError,
			wantTrace:     2,
			wantError:     true,
		},
	}

	evaluator := matchcondition.NewEvaluator()
	authorizer := mustAuthorizer(t, nil)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := evaluator.Evaluate(
				context.Background(),
				testWebhook(test.conditions, test.failurePolicy),
				testRequest(),
				testInvocation(),
				authorizer,
			)
			assertDeterminateResult(t, got, test.wantOutcome, test.wantReason, test.wantTrace, test.wantError)
		})
	}
}

func TestEvaluateMatchConditionsProblemTaxonomy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
		authorizer fixture.Authorizer
		options    []matchcondition.Option
		wantReason contract.ReasonCode
	}{
		{
			name:       "compile",
			expression: "unknownVariable == true",
			authorizer: mustAuthorizer(t, nil),
			wantReason: contract.ReasonCodeCELCompileError,
		},
		{
			name:       "runtime",
			expression: "1 / 0 == 0",
			authorizer: mustAuthorizer(t, nil),
			wantReason: contract.ReasonCodeCELRuntimeError,
		},
		{
			name:       "cost budget",
			expression: "request.resource.group == 'apps'",
			authorizer: mustAuthorizer(t, nil),
			options:    []matchcondition.Option{matchcondition.WithCostBudgetForTest(0)},
			wantReason: contract.ReasonCodeCELCostBudgetExceeded,
		},
		{
			name:       "explicit authorizer error",
			expression: authorizationExpression(),
			authorizer: mustAuthorizer(t, []contract.AuthorizationDecision{authorizationDecision(contract.AuthorizationVerdictError)}),
			wantReason: contract.ReasonCodeCELAuthorizationError,
		},
	}

	evaluator := matchcondition.NewEvaluator()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := evaluator.Evaluate(
				context.Background(),
				testWebhook(
					[]admissionregistrationv1.MatchCondition{{Name: test.name, Expression: test.expression}},
					admissionregistrationv1.Fail,
				),
				testRequest(),
				testInvocation(),
				test.authorizer,
				test.options...,
			)
			assertDeterminateResult(t, got, contract.OutcomeRejectedBeforeCall, test.wantReason, 2, true)
			if !errors.Is(got.Err, contract.ErrKubernetesEvaluation) {
				t.Errorf("Evaluate() error = %v, want ErrKubernetesEvaluation", got.Err)
			}
		})
	}
}

func TestEvaluateMatchConditionsAuthorizationDecisions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		verdict     contract.AuthorizationVerdict
		wantOutcome contract.Outcome
	}{
		{name: "allow", verdict: contract.AuthorizationVerdictAllow, wantOutcome: contract.OutcomeCalled},
		{name: "deny", verdict: contract.AuthorizationVerdictDeny, wantOutcome: contract.OutcomeSkipped},
		{name: "no opinion", verdict: contract.AuthorizationVerdictNoOpinion, wantOutcome: contract.OutcomeSkipped},
	}

	evaluator := matchcondition.NewEvaluator()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := evaluator.Evaluate(
				context.Background(),
				testWebhook(
					[]admissionregistrationv1.MatchCondition{{Name: "authorization", Expression: authorizationExpression()}},
					admissionregistrationv1.Fail,
				),
				testRequest(),
				testInvocation(),
				mustAuthorizer(t, []contract.AuthorizationDecision{authorizationDecision(test.verdict)}),
			)
			wantReason := contract.ReasonCodeMatchConditionsTrue
			if test.wantOutcome == contract.OutcomeSkipped {
				wantReason = contract.ReasonCodeMatchConditionFalse
			}
			assertDeterminateResult(t, got, test.wantOutcome, wantReason, 2, false)
		})
	}
}

func TestEvaluateMatchConditionsMissingAuthorizationIsIndeterminate(t *testing.T) {
	t.Parallel()

	evaluator := matchcondition.NewEvaluator()
	for _, failurePolicy := range []admissionregistrationv1.FailurePolicyType{
		admissionregistrationv1.Fail,
		admissionregistrationv1.Ignore,
	} {
		t.Run(string(failurePolicy), func(t *testing.T) {
			got := evaluator.Evaluate(
				context.Background(),
				testWebhook(
					[]admissionregistrationv1.MatchCondition{{Name: "missing", Expression: authorizationExpression()}},
					failurePolicy,
				),
				testRequest(),
				testInvocation(),
				mustAuthorizer(t, nil),
			)
			if got.Determination != contract.DeterminationIndeterminate || got.Outcome != nil {
				t.Errorf("Evaluate() determination/outcome = (%q, %#v), want indeterminate without outcome", got.Determination, got.Outcome)
			}
			if got.ReasonCode != contract.ReasonCodeAuthorizationContextMissing {
				t.Errorf("Evaluate() ReasonCode = %q, want %q", got.ReasonCode, contract.ReasonCodeAuthorizationContextMissing)
			}
			if !errors.Is(got.Err, contract.ErrMissingContext) || errors.Is(got.Err, contract.ErrKubernetesEvaluation) {
				t.Errorf("Evaluate() error = %v, want only ErrMissingContext", got.Err)
			}
			assertTerminalSummary(t, got.Trace, contract.TraceResultIndeterminate, contract.ReasonCodeAuthorizationContextMissing)
		})
	}
}

func TestEvaluateMatchConditionsAuthorizationSelectorsAreUnsupported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
	}{
		{
			name:       "field selector",
			expression: "authorizer.group('apps').resource('deployments').fieldSelector('metadata.name=demo').check('list').allowed()",
		},
		{
			name:       "label selector",
			expression: "authorizer.group('apps').resource('deployments').labelSelector('app=demo').check('list').allowed()",
		},
	}

	evaluator := matchcondition.NewEvaluator()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, failurePolicy := range []admissionregistrationv1.FailurePolicyType{
				admissionregistrationv1.Fail,
				admissionregistrationv1.Ignore,
			} {
				t.Run(string(failurePolicy), func(t *testing.T) {
					got := evaluator.Evaluate(
						context.Background(),
						testWebhook(
							[]admissionregistrationv1.MatchCondition{{Name: test.name, Expression: test.expression}},
							failurePolicy,
						),
						testRequest(),
						testInvocation(),
						mustAuthorizer(t, nil),
					)
					if got.Determination != contract.DeterminationUnsupported || got.Outcome != nil {
						t.Errorf("Evaluate() determination/outcome = (%q, %#v), want unsupported without outcome", got.Determination, got.Outcome)
					}
					if got.ReasonCode != contract.ReasonCodeCapabilityOutsideProfile {
						t.Errorf("Evaluate() ReasonCode = %q, want %q", got.ReasonCode, contract.ReasonCodeCapabilityOutsideProfile)
					}
					if !errors.Is(got.Err, contract.ErrUnsupportedCapability) || errors.Is(got.Err, contract.ErrKubernetesEvaluation) {
						t.Errorf("Evaluate() error = %v, want only ErrUnsupportedCapability", got.Err)
					}
					assertTerminalSummary(t, got.Trace, contract.TraceResultUnsupported, contract.ReasonCodeCapabilityOutsideProfile)
				})
			}
		})
	}
}

func TestEvaluateMatchConditionsAuthorizationErrorStillUsesFailurePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		failurePolicy admissionregistrationv1.FailurePolicyType
		wantOutcome   contract.Outcome
	}{
		{failurePolicy: admissionregistrationv1.Fail, wantOutcome: contract.OutcomeRejectedBeforeCall},
		{failurePolicy: admissionregistrationv1.Ignore, wantOutcome: contract.OutcomeSkipped},
	}
	for _, test := range tests {
		t.Run(string(test.failurePolicy), func(t *testing.T) {
			got := matchcondition.NewEvaluator().Evaluate(
				context.Background(),
				testWebhook(
					[]admissionregistrationv1.MatchCondition{{Name: "authorization", Expression: authorizationExpression()}},
					test.failurePolicy,
				),
				testRequest(),
				testInvocation(),
				mustAuthorizer(t, []contract.AuthorizationDecision{authorizationDecision(contract.AuthorizationVerdictError)}),
			)
			assertDeterminateResult(t, got, test.wantOutcome, contract.ReasonCodeCELAuthorizationError, 2, true)
			if !errors.Is(got.Err, contract.ErrKubernetesEvaluation) || errors.Is(got.Err, contract.ErrUnsupportedCapability) {
				t.Errorf("Evaluate() error = %v, want only ErrKubernetesEvaluation", got.Err)
			}
		})
	}
}

func TestEvaluateMatchConditionsFalseOverridesUnsupportedAuthorization(t *testing.T) {
	t.Parallel()

	got := matchcondition.NewEvaluator().Evaluate(
		context.Background(),
		testWebhook(
			[]admissionregistrationv1.MatchCondition{
				{Name: "unsupported", Expression: "authorizer.group('apps').resource('deployments').fieldSelector('metadata.name=demo').check('list').allowed()"},
				{Name: "false", Expression: "false"},
			},
			admissionregistrationv1.Fail,
		),
		testRequest(),
		testInvocation(),
		mustAuthorizer(t, nil),
	)
	assertDeterminateResult(t, got, contract.OutcomeSkipped, contract.ReasonCodeMatchConditionFalse, 3, false)
}

func TestEvaluateMatchConditionsFalseOverridesMissingAuthorization(t *testing.T) {
	t.Parallel()

	got := matchcondition.NewEvaluator().Evaluate(
		context.Background(),
		testWebhook(
			[]admissionregistrationv1.MatchCondition{
				{Name: "missing", Expression: authorizationExpression()},
				{Name: "false", Expression: "false"},
			},
			admissionregistrationv1.Fail,
		),
		testRequest(),
		testInvocation(),
		mustAuthorizer(t, nil),
	)
	assertDeterminateResult(t, got, contract.OutcomeSkipped, contract.ReasonCodeMatchConditionFalse, 3, false)
}

func assertDeterminateResult(
	t *testing.T,
	got matchcondition.Result,
	wantOutcome contract.Outcome,
	wantReason contract.ReasonCode,
	wantTrace int,
	wantError bool,
) {
	t.Helper()
	if got.Determination != contract.DeterminationDeterminate || got.Outcome == nil || *got.Outcome != wantOutcome {
		t.Errorf("Evaluate() determination/outcome = (%q, %#v), want determinate/%q", got.Determination, got.Outcome, wantOutcome)
	}
	if got.ReasonCode != wantReason {
		t.Errorf("Evaluate() ReasonCode = %q, want %q", got.ReasonCode, wantReason)
	}
	if len(got.Trace) != wantTrace {
		t.Fatalf("Evaluate() trace length = %d, want %d", len(got.Trace), wantTrace)
	}
	if (got.Err != nil) != wantError {
		t.Errorf("Evaluate() error = %v, wantError %t", got.Err, wantError)
	}
	wantTraceResult := contract.TraceResultTrue
	if wantOutcome == contract.OutcomeSkipped && wantReason == contract.ReasonCodeMatchConditionFalse {
		wantTraceResult = contract.TraceResultFalse
	} else if wantReason != contract.ReasonCodeMatchConditionsTrue {
		wantTraceResult = contract.TraceResultError
	}
	assertTerminalSummary(t, got.Trace, wantTraceResult, wantReason)
}

func assertTerminalSummary(
	t *testing.T,
	trace []contract.TraceStep,
	wantResult contract.TraceResult,
	wantReason contract.ReasonCode,
) {
	t.Helper()
	terminalCount := 0
	for i, step := range trace {
		if step.Sequence != i {
			t.Errorf("trace[%d].Sequence = %d, want %d", i, step.Sequence, i)
		}
		if err := step.Validate(); err != nil {
			t.Errorf("trace[%d].Validate() error = %v", i, err)
		}
		if step.Terminal {
			terminalCount++
		}
	}
	last := trace[len(trace)-1]
	if terminalCount != 1 || !last.Terminal || last.Result != wantResult || last.ReasonCode != wantReason {
		t.Errorf("terminal summary = (%d, %t, %q, %q), want (1, true, %q, %q)", terminalCount, last.Terminal, last.Result, last.ReasonCode, wantResult, wantReason)
	}
}

func testWebhook(
	conditions []admissionregistrationv1.MatchCondition,
	failurePolicy admissionregistrationv1.FailurePolicyType,
) normalize.NormalizedWebhook {
	return normalize.NormalizedWebhook{
		Name:            "example.example.com",
		SourcePath:      ".configuration.validatingWebhookConfiguration.webhooks[0]",
		MatchConditions: conditions,
		FailurePolicy:   failurePolicy,
	}
}

func testRequest() normalize.RequestContext {
	return normalize.RequestContext{
		Operation: admissionv1.Create,
		Kind: schema.GroupVersionKind{
			Group: "apps", Version: "v1", Kind: "Deployment",
		},
		Resource: schema.GroupVersionResource{
			Group: "apps", Version: "v1", Resource: "deployments",
		},
		RequestKind: schema.GroupVersionKind{
			Group: "apps", Version: "v1", Kind: "Deployment",
		},
		RequestResource: schema.GroupVersionResource{
			Group: "apps", Version: "v1", Resource: "deployments",
		},
		Namespace: "default",
		Name:      "demo",
	}
}

func testInvocation() matchcondition.Invocation {
	return matchcondition.Invocation{
		Resource: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		Kind:     schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
	}
}

func authorizationExpression() string {
	return "authorizer.group('apps').resource('deployments').namespace('default').name('demo').check('get').allowed()"
}

func authorizationDecision(verdict contract.AuthorizationVerdict) contract.AuthorizationDecision {
	return contract.AuthorizationDecision{
		Query: contract.AuthorizationQuery{
			Resource: &contract.ResourceAuthorizationQuery{
				Verb:       "get",
				APIGroup:   "apps",
				APIVersion: "*",
				Resource:   "deployments",
				Namespace:  "default",
				Name:       "demo",
			},
		},
		Verdict: verdict,
	}
}

func mustAuthorizer(t *testing.T, decisions []contract.AuthorizationDecision) fixture.Authorizer {
	t.Helper()
	authorizer, err := fixture.NewAuthorizer(decisions)
	if err != nil {
		t.Fatalf("NewAuthorizer() error = %v", err)
	}
	return authorizer
}
