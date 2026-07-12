package kube136_test

import (
	"errors"
	"testing"

	"github.com/silbaram/admitrace/internal/compat/kube136"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNamespaceSelectorMatchesKubernetesBranches(t *testing.T) {
	t.Parallel()

	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"environment": "prod"}}
	loadFailure := errors.New("must not load namespace labels")
	tests := []struct {
		name       string
		selector   *metav1.LabelSelector
		mode       kube136.NamespaceContextMode
		labels     map[string]string
		loadErr    error
		wantMatch  bool
		wantErr    error
		wantLoaded bool
	}{
		{
			name:       "Namespace request matches request object labels",
			selector:   selector,
			mode:       kube136.NamespaceContextModeRequestObject,
			labels:     map[string]string{"environment": "prod"},
			wantMatch:  true,
			wantLoaded: true,
		},
		{
			name:       "namespaced request does not match fixture labels",
			selector:   selector,
			mode:       kube136.NamespaceContextModeFixture,
			labels:     map[string]string{"environment": "dev"},
			wantLoaded: true,
		},
		{
			name:      "cluster scoped request bypasses selector parsing and lookup",
			selector:  invalidSelector(),
			mode:      kube136.NamespaceContextModeNotRequired,
			loadErr:   loadFailure,
			wantMatch: true,
		},
		{
			name:      "empty selector bypasses fixture lookup",
			selector:  &metav1.LabelSelector{},
			mode:      kube136.NamespaceContextModeFixture,
			loadErr:   loadFailure,
			wantMatch: true,
		},
		{
			name:      "omitted selector defaults to match-all without fixture lookup",
			mode:      kube136.NamespaceContextModeFixture,
			loadErr:   loadFailure,
			wantMatch: true,
		},
		{
			name:     "invalid selector is an evaluation error before lookup",
			selector: invalidSelector(),
			mode:     kube136.NamespaceContextModeFixture,
			loadErr:  loadFailure,
			wantErr:  errors.New("selector error"),
		},
		{
			name:       "fixture lookup error is preserved",
			selector:   selector,
			mode:       kube136.NamespaceContextModeFixture,
			loadErr:    loadFailure,
			wantErr:    loadFailure,
			wantLoaded: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			loaded := false
			got, err := kube136.NamespaceSelectorMatches(test.selector, test.mode, func() (map[string]string, error) {
				loaded = true
				return test.labels, test.loadErr
			})
			if got != test.wantMatch {
				t.Errorf("NamespaceSelectorMatches() = %t, want %t", got, test.wantMatch)
			}
			if test.wantErr == nil && err != nil {
				t.Errorf("NamespaceSelectorMatches() error = %v, want nil", err)
			}
			if test.wantErr != nil && err == nil {
				t.Error("NamespaceSelectorMatches() error = nil, want error")
			}
			if errors.Is(test.wantErr, loadFailure) && !errors.Is(err, loadFailure) {
				t.Errorf("NamespaceSelectorMatches() error = %v, want preserved load error", err)
			}
			if loaded != test.wantLoaded {
				t.Errorf("namespace labels loaded = %t, want %t", loaded, test.wantLoaded)
			}
		})
	}
}

func TestObjectSelectorMatchesObjectOrOldObject(t *testing.T) {
	t.Parallel()

	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"selected": "true"}}
	tests := []struct {
		name             string
		selector         *metav1.LabelSelector
		objectLabels     map[string]string
		objectPresent    bool
		oldObjectLabels  map[string]string
		oldObjectPresent bool
		want             bool
		wantErr          bool
	}{
		{
			name:          "CREATE matches object with absent oldObject",
			selector:      selector,
			objectLabels:  map[string]string{"selected": "true"},
			objectPresent: true,
			want:          true,
		},
		{
			name:             "DELETE matches oldObject with null object",
			selector:         selector,
			oldObjectLabels:  map[string]string{"selected": "true"},
			oldObjectPresent: true,
			want:             true,
		},
		{
			name:             "either object match uses OR",
			selector:         selector,
			objectLabels:     map[string]string{"selected": "false"},
			objectPresent:    true,
			oldObjectLabels:  map[string]string{"selected": "true"},
			oldObjectPresent: true,
			want:             true,
		},
		{
			name:             "both present and unmatched",
			selector:         selector,
			objectLabels:     map[string]string{"selected": "false"},
			objectPresent:    true,
			oldObjectLabels:  map[string]string{"selected": "false"},
			oldObjectPresent: true,
		},
		{
			name:     "empty selector matches two null payloads",
			selector: &metav1.LabelSelector{},
			want:     true,
		},
		{
			name: "omitted selector defaults to match-all with absent payloads",
			want: true,
		},
		{
			name:     "invalid selector returns error",
			selector: invalidSelector(),
			wantErr:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := kube136.ObjectSelectorMatches(
				test.selector,
				test.objectLabels,
				test.objectPresent,
				test.oldObjectLabels,
				test.oldObjectPresent,
			)
			if got != test.want {
				t.Errorf("ObjectSelectorMatches() = %t, want %t", got, test.want)
			}
			if (err != nil) != test.wantErr {
				t.Errorf("ObjectSelectorMatches() error = %v, want error presence %t", err, test.wantErr)
			}
		})
	}
}

func TestNamespaceSelectorMatchesRejectsMissingLoader(t *testing.T) {
	t.Parallel()

	matched, err := kube136.NamespaceSelectorMatches(
		&metav1.LabelSelector{MatchLabels: map[string]string{"environment": "prod"}},
		kube136.NamespaceContextModeFixture,
		nil,
	)
	if matched {
		t.Error("NamespaceSelectorMatches() = true, want false")
	}
	if err == nil {
		t.Fatal("NamespaceSelectorMatches() error = nil, want error")
	}
}

func invalidSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
		Key:      "environment",
		Operator: metav1.LabelSelectorOperator("Between"),
	}}}
}
