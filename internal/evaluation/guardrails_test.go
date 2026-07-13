package evaluation_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	"github.com/silbaram/admitrace/internal/fixture"
	"github.com/silbaram/admitrace/internal/normalize"
	"github.com/silbaram/admitrace/internal/render"
	authenticationv1 "k8s.io/api/authentication/v1"
)

func TestRedactionOfflineDeterminismSensitiveValuesStayOutOfResults(t *testing.T) {
	t.Parallel()

	const (
		secretData            = "secret-payload-do-not-copy"
		username              = "user-info-username-do-not-copy"
		userUID               = "user-info-uid-do-not-copy"
		userGroup             = "user-info-group-do-not-copy"
		userExtra             = "user-info-extra-do-not-copy"
		authorizationIdentity = "authorization-identity-do-not-copy"
		authorizationReason   = "authorization-reason-do-not-copy"
	)

	webhook := testWebhook(
		contract.ConfigurationKindValidating,
		0,
		"redaction.example.com",
		"authorizer.group('apps').resource('deployments').namespace('default').name('"+authorizationIdentity+"').check('get').allowed()",
	)
	snapshot := testSnapshot(t, contract.ConfigurationKindValidating, []normalize.NormalizedWebhook{webhook})
	snapshot.Request.Object = normalize.ObjectSnapshot{
		State: normalize.ObjectSnapshotObject,
		Raw:   []byte(`{"apiVersion":"v1","kind":"Secret","data":{"token":"` + secretData + `"}}`),
	}
	snapshot.Request.UserInfo = authenticationv1.UserInfo{
		Username: username,
		UID:      userUID,
		Groups:   []string{userGroup},
		Extra:    map[string]authenticationv1.ExtraValue{"identity.example.com/raw": {userExtra}},
	}
	authorizer, err := fixture.NewAuthorizer([]contract.AuthorizationDecision{{
		Query: contract.AuthorizationQuery{Resource: &contract.ResourceAuthorizationQuery{
			Verb:       "get",
			APIGroup:   "apps",
			APIVersion: "*",
			Resource:   "deployments",
			Namespace:  "default",
			Name:       authorizationIdentity,
		}},
		Verdict: contract.AuthorizationVerdictAllow,
		Reason:  authorizationReason,
	}})
	if err != nil {
		t.Fatalf("NewAuthorizer() error = %v", err)
	}
	snapshot.Authorizer = authorizer

	result := evaluation.NewEvaluator().Evaluate(context.Background(), snapshot)
	wantOutcome := contract.OutcomeCalled
	if got := result.Webhooks[0]; got.Determination != contract.DeterminationDeterminate || got.Outcome == nil || *got.Outcome != wantOutcome {
		t.Fatalf("determination/outcome = (%q, %#v), want determinate/%q", got.Determination, got.Outcome, wantOutcome)
	}
	firstJSON, err := render.JSON(result)
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	text, err := render.Text(result)
	if err != nil {
		t.Fatalf("Text() error = %v", err)
	}
	for _, sensitive := range []string{
		secretData,
		username,
		userUID,
		userGroup,
		userExtra,
		authorizationIdentity,
		authorizationReason,
	} {
		if bytes.Contains(firstJSON, []byte(sensitive)) {
			t.Errorf("JSON output contains sensitive value %q", sensitive)
		}
		if strings.Contains(string(text), sensitive) {
			t.Errorf("text output contains sensitive value %q", sensitive)
		}
	}

	for i := 0; i < 25; i++ {
		got, err := render.JSON(evaluation.NewEvaluator().Evaluate(context.Background(), snapshot))
		if err != nil {
			t.Fatalf("JSON() iteration %d error = %v", i, err)
		}
		equal := bytes.Equal(got, firstJSON)
		if !equal {
			t.Fatalf("bytes.Equal(canonical JSON iteration %d, first) = %t, want true", i, equal)
		}
	}
}

func TestRedactionOfflineDeterminismMissingAuthorizationReferenceIsRedacted(t *testing.T) {
	t.Parallel()

	const identity = "raw-authorization-object-name"
	webhook := testWebhook(
		contract.ConfigurationKindValidating,
		0,
		"missing-authz.example.com",
		"authorizer.group('apps').resource('deployments').namespace('private').name('"+identity+"').check('get').allowed()",
	)
	result := evaluation.NewEvaluator().Evaluate(
		context.Background(),
		testSnapshot(t, contract.ConfigurationKindValidating, []normalize.NormalizedWebhook{webhook}),
	)
	data, err := render.JSON(result)
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if bytes.Contains(data, []byte(identity)) {
		t.Errorf("JSON output contains authorization identity %q", identity)
	}
	if got := result.Webhooks[0].Determination; got != contract.DeterminationIndeterminate {
		t.Errorf("determination = %q, want %q", got, contract.DeterminationIndeterminate)
	}
	if len(result.Webhooks[0].Diagnostics) != 1 || result.Webhooks[0].Diagnostics[0].MissingContext == nil {
		t.Fatalf("diagnostics = %#v, want one missing authorization context", result.Webhooks[0].Diagnostics)
	}
}
