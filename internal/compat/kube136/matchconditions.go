package kube136

import (
	"context"
	"errors"
	"sync"

	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/admission"
	celplugin "k8s.io/apiserver/pkg/admission/plugin/cel"
	stagingmatchconditions "k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apiservercel "k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/environment"
)

// DefaultMatchConditionCostBudget is the Kubernetes 1.36 runtime budget for
// one webhook's matchConditions.
const DefaultMatchConditionCostBudget int64 = celconfig.RuntimeCELCostBudgetMatchConditions

// MatchConditionProblem identifies an instrumented CEL evaluation problem.
type MatchConditionProblem string

const (
	// MatchConditionProblemNone indicates a boolean CEL result without a problem.
	MatchConditionProblemNone MatchConditionProblem = ""
	// MatchConditionProblemCompile identifies an expression compilation failure.
	MatchConditionProblemCompile MatchConditionProblem = "compile"
	// MatchConditionProblemRuntime identifies a CEL runtime evaluation failure.
	MatchConditionProblemRuntime MatchConditionProblem = "runtime"
	// MatchConditionProblemCost identifies exhaustion of the explicit runtime budget.
	MatchConditionProblemCost MatchConditionProblem = "cost"
	// MatchConditionProblemAuthorization identifies an explicit fixture authorizer error.
	MatchConditionProblemAuthorization MatchConditionProblem = "authorization"
	// MatchConditionProblemAuthorizationMissing identifies an absent fixture authorizer decision.
	MatchConditionProblemAuthorizationMissing MatchConditionProblem = "authorization-missing"
)

// MatchConditionDecision identifies the official matcher result, with missing
// fixture context preserved outside failurePolicy.
type MatchConditionDecision string

const (
	// MatchConditionDecisionCall indicates all configured conditions matched.
	MatchConditionDecisionCall MatchConditionDecision = "call"
	// MatchConditionDecisionSkipFalse indicates a condition evaluated false.
	MatchConditionDecisionSkipFalse MatchConditionDecision = "skip-false"
	// MatchConditionDecisionSkipError indicates failurePolicy Ignore handled an error.
	MatchConditionDecisionSkipError MatchConditionDecision = "skip-error"
	// MatchConditionDecisionReject indicates failurePolicy Fail handled an error.
	MatchConditionDecisionReject MatchConditionDecision = "reject"
	// MatchConditionDecisionIndeterminate indicates a required fixture decision was missing.
	MatchConditionDecisionIndeterminate MatchConditionDecision = "indeterminate"
)

// MatchConditionObservation records one condition's raw Kubernetes CEL result.
type MatchConditionObservation struct {
	Index   int
	Name    string
	Value   *bool
	Problem MatchConditionProblem
	Err     error
}

// MatchConditionsResult contains the official reducer decision and the raw
// condition observations used to explain it.
type MatchConditionsResult struct {
	Decision         MatchConditionDecision
	ControllingIndex int
	Observations     []MatchConditionObservation
	Err              error
}

// MatchConditionEvaluator compiles expressions with the Kubernetes 1.36
// admission CEL environment.
type MatchConditionEvaluator struct {
	compiler celplugin.ConditionCompiler
}

// NewMatchConditionEvaluator creates an evaluator using the Kubernetes 1.36
// default compatibility environment.
func NewMatchConditionEvaluator() MatchConditionEvaluator {
	env := environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion())
	return MatchConditionEvaluator{compiler: celplugin.NewConditionCompiler(env)}
}

// Evaluate applies the Kubernetes 1.36 webhook matchConditions reducer while
// retaining raw results and fixture authorizer categories for explanation.
func (e MatchConditionEvaluator) Evaluate(
	ctx context.Context,
	conditions []admissionregistrationv1.MatchCondition,
	failurePolicy admissionregistrationv1.FailurePolicyType,
	attributes *admission.VersionedAttributes,
	authz authorizer.Authorizer,
	costBudget int64,
) MatchConditionsResult {
	filter := newInstrumentedConditionFilter(e.compiler, conditions, costBudget)
	matcher := stagingmatchconditions.NewMatcher(filter, &failurePolicy, "webhook", "offline", "")
	matched := matcher.Match(ctx, attributes, nil, authz)

	result := MatchConditionsResult{
		ControllingIndex: -1,
		Observations:     append([]MatchConditionObservation(nil), filter.observations...),
		Err:              matched.Error,
	}
	if falseIndex := firstFalseObservation(result.Observations); falseIndex >= 0 {
		result.Decision = MatchConditionDecisionSkipFalse
		result.ControllingIndex = falseIndex
		result.Err = nil
		return result
	}
	if missingIndex := firstProblemObservation(result.Observations, MatchConditionProblemAuthorizationMissing); missingIndex >= 0 {
		result.Decision = MatchConditionDecisionIndeterminate
		result.ControllingIndex = missingIndex
		result.Err = result.Observations[missingIndex].Err
		return result
	}
	if matched.Error != nil {
		result.Decision = MatchConditionDecisionReject
		result.ControllingIndex = firstProblemIndex(result.Observations)
		return result
	}
	if matched.Matches {
		result.Decision = MatchConditionDecisionCall
		return result
	}
	result.Decision = MatchConditionDecisionSkipError
	result.ControllingIndex = firstProblemIndex(result.Observations)
	return result
}

type instrumentedConditionFilter struct {
	evaluators   []celplugin.ConditionEvaluator
	conditions   []*stagingmatchconditions.MatchCondition
	costBudget   int64
	observations []MatchConditionObservation
}

func newInstrumentedConditionFilter(
	compiler celplugin.ConditionCompiler,
	conditions []admissionregistrationv1.MatchCondition,
	costBudget int64,
) *instrumentedConditionFilter {
	filter := &instrumentedConditionFilter{
		evaluators: make([]celplugin.ConditionEvaluator, len(conditions)),
		conditions: make([]*stagingmatchconditions.MatchCondition, len(conditions)),
		costBudget: costBudget,
	}
	for i, condition := range conditions {
		accessor := &stagingmatchconditions.MatchCondition{
			Name:       condition.Name,
			Expression: condition.Expression,
		}
		filter.conditions[i] = accessor
		filter.evaluators[i] = compiler.CompileCondition(
			[]celplugin.ExpressionAccessor{accessor},
			celplugin.OptionalVariableDeclarations{HasAuthorizer: true},
			environment.StoredExpressions,
		)
	}
	return filter
}

// ForInput evaluates conditions in declaration order with one shared explicit cost budget.
func (f *instrumentedConditionFilter) ForInput(
	ctx context.Context,
	attributes *admission.VersionedAttributes,
	request *admissionv1.AdmissionRequest,
	bindings celplugin.OptionalVariableBindings,
	namespace *corev1.Namespace,
	_ int64,
) ([]celplugin.EvaluationResult, int64, error) {
	f.observations = make([]MatchConditionObservation, 0, len(f.evaluators))
	evaluations := make([]celplugin.EvaluationResult, 0, len(f.evaluators))
	remainingBudget := f.costBudget
	for i, evaluator := range f.evaluators {
		recorder := &recordingAuthorizer{delegate: bindings.Authorizer}
		conditionBindings := bindings
		conditionBindings.Authorizer = recorder
		conditionEvaluations, remaining, err := evaluator.ForInput(
			ctx,
			attributes,
			request,
			conditionBindings,
			namespace,
			remainingBudget,
		)
		if err != nil {
			problem := MatchConditionProblemRuntime
			if errors.Is(err, apiservercel.ErrOutOfBudget) {
				problem = MatchConditionProblemCost
			}
			f.observations = append(f.observations, MatchConditionObservation{
				Index:   i,
				Name:    f.conditions[i].Name,
				Problem: problem,
				Err:     err,
			})
			return nil, -1, err
		}
		remainingBudget = remaining

		evaluation := conditionEvaluations[0]
		observation := MatchConditionObservation{Index: i, Name: f.conditions[i].Name}
		if compilationErrors := evaluator.CompilationErrors(); len(compilationErrors) > 0 {
			observation.Problem = MatchConditionProblemCompile
			observation.Err = errors.Join(compilationErrors...)
		}
		authorizationErrors := recorder.Errors()
		if len(authorizationErrors) > 0 {
			observation.Problem = authorizationProblem()
			observation.Err = errors.Join(authorizationErrors...)
			evaluation.Error = observation.Err
			evaluation.EvalResult = nil
		} else if evaluation.Error != nil && observation.Problem == MatchConditionProblemNone {
			observation.Problem = MatchConditionProblemRuntime
			observation.Err = evaluation.Error
		}
		if evaluation.EvalResult != nil {
			if value, ok := evaluation.EvalResult.Value().(bool); ok {
				observation.Value = &value
			}
		}
		f.observations = append(f.observations, observation)
		evaluations = append(evaluations, evaluation)
	}
	return evaluations, remainingBudget, nil
}

// CompilationErrors returns every condition compilation error in declaration order.
func (f *instrumentedConditionFilter) CompilationErrors() []error {
	var result []error
	for _, evaluator := range f.evaluators {
		result = append(result, evaluator.CompilationErrors()...)
	}
	return result
}

type recordingAuthorizer struct {
	delegate authorizer.Authorizer
	mu       sync.Mutex
	errors   []error
}

// Authorize delegates the request and records errors for result classification.
func (a *recordingAuthorizer) Authorize(
	ctx context.Context,
	attributes authorizer.Attributes,
) (authorizer.Decision, string, error) {
	decision, reason, err := a.delegate.Authorize(ctx, attributes)
	if err != nil {
		a.mu.Lock()
		a.errors = append(a.errors, err)
		a.mu.Unlock()
	}
	return decision, reason, err
}

// Errors returns a copy of the recorded authorization errors.
func (a *recordingAuthorizer) Errors() []error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]error(nil), a.errors...)
}

func authorizationProblem() MatchConditionProblem {
	return MatchConditionProblemAuthorization
}

func firstFalseObservation(observations []MatchConditionObservation) int {
	for i, observation := range observations {
		if observation.Value != nil && !*observation.Value {
			return i
		}
	}
	return -1
}

func firstProblemObservation(observations []MatchConditionObservation, problem MatchConditionProblem) int {
	for i, observation := range observations {
		if observation.Problem == problem {
			return i
		}
	}
	return -1
}

func firstProblemIndex(observations []MatchConditionObservation) int {
	for i, observation := range observations {
		if observation.Problem != MatchConditionProblemNone {
			return i
		}
	}
	return -1
}

var _ celplugin.ConditionEvaluator = (*instrumentedConditionFilter)(nil)
var _ authorizer.Authorizer = (*recordingAuthorizer)(nil)
