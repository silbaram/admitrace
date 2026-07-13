package matchcondition_test

import (
	"context"
	"errors"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/matchcondition"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

func TestResourceLimitsCELBudgetDiagnosticIsStable(t *testing.T) {
	t.Parallel()

	evaluate := func() matchcondition.Result {
		return matchcondition.NewEvaluator().Evaluate(
			context.Background(),
			testWebhook(
				[]admissionregistrationv1.MatchCondition{{Name: "budget", Expression: "request.resource.group == 'apps'"}},
				admissionregistrationv1.Fail,
			),
			testRequest(),
			testInvocation(),
			mustAuthorizer(t, nil),
			matchcondition.WithCostBudgetForTest(0),
		)
	}

	first := evaluate()
	second := evaluate()
	for i, got := range []matchcondition.Result{first, second} {
		if got.ReasonCode != contract.ReasonCodeCELCostBudgetExceeded {
			t.Errorf("Evaluate() result %d reason = %q, want %q", i, got.ReasonCode, contract.ReasonCodeCELCostBudgetExceeded)
		}
		if !errors.Is(got.Err, contract.ErrKubernetesEvaluation) {
			t.Errorf("Evaluate() result %d error = %v, want ErrKubernetesEvaluation", i, got.Err)
		}
		assertTerminalSummary(t, got.Trace, contract.TraceResultError, contract.ReasonCodeCELCostBudgetExceeded)
	}
	if first.Err == nil || second.Err == nil || first.Err.Error() != second.Err.Error() {
		t.Errorf("repeated CEL budget errors = (%v, %v), want stable text", first.Err, second.Err)
	}
}
