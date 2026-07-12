package fixture_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	"k8s.io/apimachinery/pkg/fields"
	kubeauthorizer "k8s.io/apiserver/pkg/authorization/authorizer"
)

func TestAuthorizerReplaysEveryVerdict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		verdict      contract.AuthorizationVerdict
		wantDecision kubeauthorizer.Decision
		wantCategory error
	}{
		{name: "allow", verdict: contract.AuthorizationVerdictAllow, wantDecision: kubeauthorizer.DecisionAllow},
		{name: "deny", verdict: contract.AuthorizationVerdictDeny, wantDecision: kubeauthorizer.DecisionDeny},
		{name: "no opinion", verdict: contract.AuthorizationVerdictNoOpinion, wantDecision: kubeauthorizer.DecisionNoOpinion},
		{name: "error", verdict: contract.AuthorizationVerdictError, wantDecision: kubeauthorizer.DecisionNoOpinion, wantCategory: contract.ErrKubernetesEvaluation},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixtureAuthorizer, err := fixture.NewAuthorizer([]contract.AuthorizationDecision{
				{
					Query: contract.AuthorizationQuery{
						Resource: resourceQuery("check-" + strings.ReplaceAll(test.name, " ", "-")),
					},
					Verdict: test.verdict,
					Reason:  "fixture reason",
				},
			})
			if err != nil {
				t.Fatalf("NewAuthorizer() error = %v", err)
			}

			attributes := resourceAttributes("check-" + strings.ReplaceAll(test.name, " ", "-"))
			gotDecision, gotReason, gotErr := fixtureAuthorizer.Authorize(context.Background(), attributes)
			if gotDecision != test.wantDecision {
				t.Errorf("Authorize() decision = %v, want %v", gotDecision, test.wantDecision)
			}
			if gotReason != "fixture reason" {
				t.Errorf("Authorize() reason = %q, want %q", gotReason, "fixture reason")
			}
			if !errors.Is(gotErr, test.wantCategory) {
				t.Errorf("Authorize() error = %v, want category %v", gotErr, test.wantCategory)
			}
			if test.wantCategory == contract.ErrKubernetesEvaluation {
				var evaluationError *contract.KubernetesEvaluationError
				if !errors.As(gotErr, &evaluationError) {
					t.Errorf("Authorize() error type = %T, want *contract.KubernetesEvaluationError", gotErr)
				}
			}
		})
	}
}

func TestAuthorizerMatchesResourceCanonicalKeyExactly(t *testing.T) {
	t.Parallel()

	query := resourceQuery("get")
	fixtureAuthorizer, err := fixture.NewAuthorizer([]contract.AuthorizationDecision{
		{Query: contract.AuthorizationQuery{Resource: query}, Verdict: contract.AuthorizationVerdictAllow},
	})
	if err != nil {
		t.Fatalf("NewAuthorizer() error = %v", err)
	}

	matching := resourceAttributes("get")
	for i := 0; i < 25; i++ {
		decision, _, err := fixtureAuthorizer.Authorize(context.Background(), matching)
		if err != nil {
			t.Fatalf("Authorize() iteration %d error = %v", i, err)
		}
		if decision != kubeauthorizer.DecisionAllow {
			t.Fatalf("Authorize() iteration %d decision = %v, want %v", i, decision, kubeauthorizer.DecisionAllow)
		}
	}

	tests := []struct {
		name   string
		mutate func(*kubeauthorizer.AttributesRecord)
	}{
		{name: "verb", mutate: func(attributes *kubeauthorizer.AttributesRecord) { attributes.Verb = "list" }},
		{name: "verb case", mutate: func(attributes *kubeauthorizer.AttributesRecord) { attributes.Verb = "GET" }},
		{name: "api group", mutate: func(attributes *kubeauthorizer.AttributesRecord) { attributes.APIGroup = "batch" }},
		{name: "api version", mutate: func(attributes *kubeauthorizer.AttributesRecord) { attributes.APIVersion = "v1" }},
		{name: "resource", mutate: func(attributes *kubeauthorizer.AttributesRecord) { attributes.Resource = "statefulsets" }},
		{name: "subresource", mutate: func(attributes *kubeauthorizer.AttributesRecord) { attributes.Subresource = "scale" }},
		{name: "namespace", mutate: func(attributes *kubeauthorizer.AttributesRecord) { attributes.Namespace = "prod" }},
		{name: "name", mutate: func(attributes *kubeauthorizer.AttributesRecord) { attributes.Name = "frontend" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attributes := *matching
			test.mutate(&attributes)
			decision, _, err := fixtureAuthorizer.Authorize(context.Background(), &attributes)
			if decision != kubeauthorizer.DecisionNoOpinion {
				t.Errorf("Authorize() decision = %v, want %v", decision, kubeauthorizer.DecisionNoOpinion)
			}
			if !errors.Is(err, contract.ErrMissingContext) {
				t.Errorf("Authorize() error = %v, want %v", err, contract.ErrMissingContext)
			}
		})
	}
}

func TestAuthorizerMatchesNonResourceCanonicalKeyExactly(t *testing.T) {
	t.Parallel()

	fixtureAuthorizer, err := fixture.NewAuthorizer([]contract.AuthorizationDecision{
		{
			Query: contract.AuthorizationQuery{
				NonResource: &contract.NonResourceAuthorizationQuery{Verb: "get", Path: "/healthz"},
			},
			Verdict: contract.AuthorizationVerdictDeny,
			Reason:  "health endpoint is restricted",
		},
	})
	if err != nil {
		t.Fatalf("NewAuthorizer() error = %v", err)
	}

	decision, reason, err := fixtureAuthorizer.Authorize(context.Background(), &kubeauthorizer.AttributesRecord{
		Verb: "get",
		Path: "/healthz",
	})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if decision != kubeauthorizer.DecisionDeny {
		t.Errorf("Authorize() decision = %v, want %v", decision, kubeauthorizer.DecisionDeny)
	}
	if reason != "health endpoint is restricted" {
		t.Errorf("Authorize() reason = %q, want %q", reason, "health endpoint is restricted")
	}

	for _, attributes := range []*kubeauthorizer.AttributesRecord{
		{Verb: "GET", Path: "/healthz"},
		{Verb: "get", Path: "healthz"},
		{Verb: "get", Path: "/readyz"},
	} {
		decision, _, err := fixtureAuthorizer.Authorize(context.Background(), attributes)
		if decision != kubeauthorizer.DecisionNoOpinion {
			t.Errorf("Authorize(%q, %q) decision = %v, want %v", attributes.Verb, attributes.Path, decision, kubeauthorizer.DecisionNoOpinion)
		}
		if !errors.Is(err, contract.ErrMissingContext) {
			t.Errorf("Authorize(%q, %q) error = %v, want %v", attributes.Verb, attributes.Path, err, contract.ErrMissingContext)
		}
	}
}

func TestExplicitNoOpinionDiffersFromMissingAuthorizationDecision(t *testing.T) {
	t.Parallel()

	fixtureAuthorizer, err := fixture.NewAuthorizer([]contract.AuthorizationDecision{
		{
			Query:   contract.AuthorizationQuery{Resource: resourceQuery("get")},
			Verdict: contract.AuthorizationVerdictNoOpinion,
			Reason:  "no fixture opinion",
		},
	})
	if err != nil {
		t.Fatalf("NewAuthorizer() error = %v", err)
	}

	decision, reason, err := fixtureAuthorizer.Authorize(context.Background(), resourceAttributes("get"))
	if err != nil {
		t.Fatalf("Authorize() explicit no-opinion error = %v", err)
	}
	if decision != kubeauthorizer.DecisionNoOpinion || reason != "no fixture opinion" {
		t.Errorf("Authorize() explicit no-opinion = (%v, %q), want (%v, %q)", decision, reason, kubeauthorizer.DecisionNoOpinion, "no fixture opinion")
	}

	decision, reason, err = fixtureAuthorizer.Authorize(context.Background(), resourceAttributes("list"))
	if decision != kubeauthorizer.DecisionNoOpinion || reason != "" {
		t.Errorf("Authorize() missing = (%v, %q), want (%v, empty)", decision, reason, kubeauthorizer.DecisionNoOpinion)
	}
	if !errors.Is(err, contract.ErrMissingContext) {
		t.Fatalf("Authorize() missing error = %v, want %v", err, contract.ErrMissingContext)
	}
	if errors.Is(err, contract.ErrKubernetesEvaluation) {
		t.Errorf("Authorize() missing error = %v, must not be a Kubernetes evaluation error", err)
	}
	var missingError *contract.MissingContextError
	if !errors.As(err, &missingError) {
		t.Errorf("Authorize() missing error type = %T, want *contract.MissingContextError", err)
	}
}

func TestNewAuthorizerRejectsDuplicateAndConflictingDecisions(t *testing.T) {
	t.Parallel()

	base := contract.AuthorizationDecision{
		Query:   contract.AuthorizationQuery{Resource: resourceQuery("get")},
		Verdict: contract.AuthorizationVerdictAllow,
		Reason:  "allowed",
	}
	tests := []struct {
		name         string
		second       contract.AuthorizationDecision
		wantCategory error
	}{
		{name: "duplicate", second: base, wantCategory: fixture.ErrDuplicateAuthorizationQuery},
		{
			name: "conflicting verdict",
			second: contract.AuthorizationDecision{
				Query:   contract.AuthorizationQuery{Resource: resourceQuery("get")},
				Verdict: contract.AuthorizationVerdictDeny,
				Reason:  "denied",
			},
			wantCategory: fixture.ErrConflictingAuthorizationDecision,
		},
		{
			name: "conflicting reason",
			second: contract.AuthorizationDecision{
				Query:   contract.AuthorizationQuery{Resource: resourceQuery("get")},
				Verdict: contract.AuthorizationVerdictAllow,
				Reason:  "different reason",
			},
			wantCategory: fixture.ErrConflictingAuthorizationDecision,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := fixture.NewAuthorizer([]contract.AuthorizationDecision{base, test.second})
			if !errors.Is(err, contract.ErrInvalidInput) {
				t.Fatalf("NewAuthorizer() error = %v, want %v", err, contract.ErrInvalidInput)
			}
			if !errors.Is(err, test.wantCategory) {
				t.Errorf("NewAuthorizer() error = %v, want category %v", err, test.wantCategory)
			}
		})
	}
}

func TestNewAuthorizerValidatesQueriesAndVerdicts(t *testing.T) {
	t.Parallel()

	validResource := resourceQuery("get")
	validNonResource := &contract.NonResourceAuthorizationQuery{Verb: "get", Path: "/healthz"}
	tests := []struct {
		name     string
		decision contract.AuthorizationDecision
	}{
		{name: "missing query", decision: contract.AuthorizationDecision{Verdict: contract.AuthorizationVerdictAllow}},
		{
			name: "both query forms",
			decision: contract.AuthorizationDecision{
				Query:   contract.AuthorizationQuery{Resource: validResource, NonResource: validNonResource},
				Verdict: contract.AuthorizationVerdictAllow,
			},
		},
		{
			name: "empty resource verb",
			decision: contract.AuthorizationDecision{
				Query:   contract.AuthorizationQuery{Resource: resourceQuery("")},
				Verdict: contract.AuthorizationVerdictAllow,
			},
		},
		{
			name: "empty resource",
			decision: contract.AuthorizationDecision{
				Query:   contract.AuthorizationQuery{Resource: &contract.ResourceAuthorizationQuery{Verb: "get"}},
				Verdict: contract.AuthorizationVerdictAllow,
			},
		},
		{
			name: "resource slash",
			decision: contract.AuthorizationDecision{
				Query:   contract.AuthorizationQuery{Resource: &contract.ResourceAuthorizationQuery{Verb: "get", Resource: "pods/status"}},
				Verdict: contract.AuthorizationVerdictAllow,
			},
		},
		{
			name: "resource whitespace",
			decision: contract.AuthorizationDecision{
				Query:   contract.AuthorizationQuery{Resource: &contract.ResourceAuthorizationQuery{Verb: "get", Resource: "pod s"}},
				Verdict: contract.AuthorizationVerdictAllow,
			},
		},
		{
			name: "empty non-resource path",
			decision: contract.AuthorizationDecision{
				Query:   contract.AuthorizationQuery{NonResource: &contract.NonResourceAuthorizationQuery{Verb: "get"}},
				Verdict: contract.AuthorizationVerdictAllow,
			},
		},
		{
			name: "non-resource path whitespace",
			decision: contract.AuthorizationDecision{
				Query:   contract.AuthorizationQuery{NonResource: &contract.NonResourceAuthorizationQuery{Verb: "get", Path: "/health z"}},
				Verdict: contract.AuthorizationVerdictAllow,
			},
		},
		{
			name: "unknown verdict",
			decision: contract.AuthorizationDecision{
				Query:   contract.AuthorizationQuery{Resource: validResource},
				Verdict: "sometimes",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := fixture.NewAuthorizer([]contract.AuthorizationDecision{test.decision})
			if !errors.Is(err, contract.ErrInvalidInput) {
				t.Errorf("NewAuthorizer() error = %v, want %v", err, contract.ErrInvalidInput)
			}
		})
	}
}

func TestAuthorizerOwnsDecisionValues(t *testing.T) {
	t.Parallel()

	query := resourceQuery("get")
	decisions := []contract.AuthorizationDecision{
		{
			Query:   contract.AuthorizationQuery{Resource: query},
			Verdict: contract.AuthorizationVerdictAllow,
			Reason:  "original reason",
		},
	}
	fixtureAuthorizer, err := fixture.NewAuthorizer(decisions)
	if err != nil {
		t.Fatalf("NewAuthorizer() error = %v", err)
	}

	query.Resource = "secrets"
	decisions[0].Verdict = contract.AuthorizationVerdictDeny
	decisions[0].Reason = "mutated reason"

	decision, reason, err := fixtureAuthorizer.Authorize(context.Background(), resourceAttributes("get"))
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if decision != kubeauthorizer.DecisionAllow || reason != "original reason" {
		t.Errorf("Authorize() = (%v, %q), want (%v, %q)", decision, reason, kubeauthorizer.DecisionAllow, "original reason")
	}
}

func TestAuthorizerRejectsNilAndSelectorBearingAttributes(t *testing.T) {
	t.Parallel()

	fixtureAuthorizer, err := fixture.NewAuthorizer(nil)
	if err != nil {
		t.Fatalf("NewAuthorizer() error = %v", err)
	}

	decision, _, err := fixtureAuthorizer.Authorize(context.Background(), nil)
	if decision != kubeauthorizer.DecisionNoOpinion {
		t.Errorf("Authorize(nil) decision = %v, want %v", decision, kubeauthorizer.DecisionNoOpinion)
	}
	if !errors.Is(err, contract.ErrInvalidInput) {
		t.Errorf("Authorize(nil) error = %v, want %v", err, contract.ErrInvalidInput)
	}
	var typedNil *kubeauthorizer.AttributesRecord
	decision, _, err = fixtureAuthorizer.Authorize(context.Background(), typedNil)
	if decision != kubeauthorizer.DecisionNoOpinion {
		t.Errorf("Authorize(typed nil) decision = %v, want %v", decision, kubeauthorizer.DecisionNoOpinion)
	}
	if !errors.Is(err, contract.ErrInvalidInput) {
		t.Errorf("Authorize(typed nil) error = %v, want %v", err, contract.ErrInvalidInput)
	}

	selector, parseErr := fields.ParseSelector("metadata.name=demo")
	if parseErr != nil {
		t.Fatalf("ParseSelector() error = %v", parseErr)
	}
	attributes := resourceAttributes("get")
	attributes.FieldSelectorRequirements = selector.Requirements()
	decision, _, err = fixtureAuthorizer.Authorize(context.Background(), attributes)
	if decision != kubeauthorizer.DecisionNoOpinion {
		t.Errorf("Authorize(selector) decision = %v, want %v", decision, kubeauthorizer.DecisionNoOpinion)
	}
	if !errors.Is(err, contract.ErrUnsupportedCapability) {
		t.Errorf("Authorize(selector) error = %v, want %v", err, contract.ErrUnsupportedCapability)
	}

	parseFailure := errors.New("field selector parse failure")
	attributes = resourceAttributes("get")
	attributes.FieldSelectorParsingErr = parseFailure
	decision, _, err = fixtureAuthorizer.Authorize(context.Background(), attributes)
	if decision != kubeauthorizer.DecisionNoOpinion {
		t.Errorf("Authorize(selector error) decision = %v, want %v", decision, kubeauthorizer.DecisionNoOpinion)
	}
	if !errors.Is(err, contract.ErrUnsupportedCapability) {
		t.Errorf("Authorize(selector error) error = %v, want %v", err, contract.ErrUnsupportedCapability)
	}
	if !errors.Is(err, parseFailure) {
		t.Errorf("Authorize(selector error) error = %v, want wrapped cause %v", err, parseFailure)
	}
}

func TestAuthorizerRejectsMalformedAttributesBeforeLookup(t *testing.T) {
	t.Parallel()

	fixtureAuthorizer, err := fixture.NewAuthorizer(nil)
	if err != nil {
		t.Fatalf("NewAuthorizer() error = %v", err)
	}
	tests := []struct {
		name       string
		attributes *kubeauthorizer.AttributesRecord
	}{
		{
			name: "resource without resource name",
			attributes: &kubeauthorizer.AttributesRecord{
				Verb:            "get",
				ResourceRequest: true,
			},
		},
		{
			name:       "non-resource without path",
			attributes: &kubeauthorizer.AttributesRecord{Verb: "get"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			decision, _, err := fixtureAuthorizer.Authorize(context.Background(), test.attributes)
			if decision != kubeauthorizer.DecisionNoOpinion {
				t.Errorf("Authorize() decision = %v, want %v", decision, kubeauthorizer.DecisionNoOpinion)
			}
			if !errors.Is(err, contract.ErrInvalidInput) {
				t.Errorf("Authorize() error = %v, want %v", err, contract.ErrInvalidInput)
			}
			if errors.Is(err, contract.ErrMissingContext) {
				t.Errorf("Authorize() error = %v, malformed attributes must not be missing context", err)
			}
		})
	}
}

func resourceQuery(verb string) *contract.ResourceAuthorizationQuery {
	return &contract.ResourceAuthorizationQuery{
		Verb:        verb,
		APIGroup:    "apps",
		APIVersion:  "*",
		Resource:    "deployments",
		Subresource: "status",
		Namespace:   "test",
		Name:        "backend",
	}
}

func resourceAttributes(verb string) *kubeauthorizer.AttributesRecord {
	return &kubeauthorizer.AttributesRecord{
		Verb:            verb,
		APIGroup:        "apps",
		APIVersion:      "*",
		Resource:        "deployments",
		Subresource:     "status",
		Namespace:       "test",
		Name:            "backend",
		ResourceRequest: true,
	}
}
