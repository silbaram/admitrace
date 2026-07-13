package fixture_test

import (
	"context"
	"errors"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/fixture"
	kubeauthorizer "k8s.io/apiserver/pkg/authorization/authorizer"
)

func FuzzFixtureContextDeterministicReplay(f *testing.F) {
	f.Add("get", "apps", "v1", "deployments", "default", "demo")
	f.Add("list", "", "v1", "pods", "team-a", "")
	f.Add("bad verb", "apps", "v1", "deployments", "default", "demo")
	f.Add("", "", "", "", "", "")

	f.Fuzz(func(t *testing.T, verb, apiGroup, apiVersion, resource, namespace, name string) {
		query := &contract.ResourceAuthorizationQuery{
			Verb:       verb,
			APIGroup:   apiGroup,
			APIVersion: apiVersion,
			Resource:   resource,
			Namespace:  namespace,
			Name:       name,
		}
		decisions := []contract.AuthorizationDecision{{
			Query:   contract.AuthorizationQuery{Resource: query},
			Verdict: contract.AuthorizationVerdictAllow,
		}}
		first, firstErr := fixture.NewAuthorizer(decisions)
		second, secondErr := fixture.NewAuthorizer(decisions)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("NewAuthorizer() error presence = (%v, %v), want equal", firstErr, secondErr)
		}
		if firstErr != nil {
			if firstErr.Error() != secondErr.Error() {
				t.Fatalf("NewAuthorizer() errors = (%q, %q), want equal", firstErr, secondErr)
			}
			if !errors.Is(firstErr, contract.ErrInvalidInput) {
				t.Fatalf("NewAuthorizer() error = %v, want ErrInvalidInput", firstErr)
			}
			return
		}

		attributes := &kubeauthorizer.AttributesRecord{
			ResourceRequest: true,
			Verb:            verb,
			APIGroup:        apiGroup,
			APIVersion:      apiVersion,
			Resource:        resource,
			Namespace:       namespace,
			Name:            name,
		}
		firstDecision, firstReason, firstErr := first.Authorize(context.Background(), attributes)
		secondDecision, secondReason, secondErr := second.Authorize(context.Background(), attributes)
		if firstDecision != secondDecision || firstReason != secondReason {
			t.Fatalf("Authorize() results = (%v, %q) and (%v, %q), want equal", firstDecision, firstReason, secondDecision, secondReason)
		}
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("Authorize() errors = (%v, %v), want equal presence", firstErr, secondErr)
		}
		if firstErr != nil && firstErr.Error() != secondErr.Error() {
			t.Fatalf("Authorize() errors = (%q, %q), want equal", firstErr, secondErr)
		}
	})
}
