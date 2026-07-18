//go:build conformance

package oracle

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/silbaram/admitrace/internal/adapter"
	"github.com/silbaram/admitrace/internal/compat/kube136"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/evaluation"
	"github.com/silbaram/admitrace/internal/hydration"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/render"
	"github.com/silbaram/admitrace/internal/resourcecatalog"
	"github.com/silbaram/admitrace/internal/scenario"
	"github.com/silbaram/admitrace/internal/snapshot"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	manifestAdapterCRDGroup      = "integration.admitrace.io"
	manifestAdapterCRDKind       = "Widget"
	manifestAdapterCRDResource   = "widgets"
	manifestAdapterNamespace     = "manifest-adapter-team"
	manifestAdapterContext       = "manifest-adapter-envtest"
	manifestAdapterWebhookName   = "manifest-adapter.oracle.admitrace.io"
	manifestAdapterConfiguration = "manifest-adapter.oracle.admitrace.io"
)

func TestManifestAdapterHydrationAndSnapshot(t *testing.T) {
	harness := startConformanceHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	installManifestAdapterCRD(t, ctx, harness)
	installManifestAdapterNamespace(t, ctx, harness)
	configuration := installManifestAdapterConfiguration(t, ctx, harness)
	selected, reader := connectManifestAdapterHydration(t, ctx, harness)
	discovery := waitForManifestAdapterDiscovery(t, ctx, reader)

	t.Run("catalog and CRD discovery", func(t *testing.T) {
		assertManifestAdapterCatalog(t, discovery)
		assertManifestAdapterCRDResolution(t, discovery)
	})
	t.Run("real API permission denial", func(t *testing.T) {
		assertManifestAdapterPermissionDenial(t, ctx, harness)
	})

	resources := decodeManifestAdapterResources(t)
	resolved, err := adapter.Resolve(ctx, resources, adapter.Options{Hydration: selected})
	if err != nil {
		t.Fatalf("adapter.Resolve(hydrated) error = %v", err)
	}
	t.Run("Namespace and configuration hydration", func(t *testing.T) {
		assertManifestAdapterHydratedResults(t, ctx, resolved)
	})

	t.Run("offline CRD remains unsupported", func(t *testing.T) {
		_, err := adapter.Resolve(ctx, resources[1:], adapter.Options{FileConfigurations: []manifest.ConfigurationInput{{
			Source:     manifest.Source{Kind: manifest.SourceKindFile, Label: "configuration.yaml", DocumentIndex: 1},
			Validating: configuration.DeepCopy(),
		}}})
		if !errors.Is(err, contract.ErrUnsupportedCapability) {
			t.Fatalf("adapter.Resolve(offline CRD) error = %v, want unsupported capability", err)
		}
	})

	t.Run("snapshot offline replay", func(t *testing.T) {
		assertManifestAdapterSnapshotReplay(t, ctx, harness, resolved.BuiltScenarios)
	})
}

func installManifestAdapterCRD(t *testing.T, ctx context.Context, harness *Harness) {
	t.Helper()
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: manifestAdapterCRDResource + "." + manifestAdapterCRDGroup},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: manifestAdapterCRDGroup,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   manifestAdapterCRDResource,
				Singular: "widget",
				Kind:     manifestAdapterCRDKind,
				ListKind: manifestAdapterCRDKind + "List",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {Type: "object", XPreserveUnknownFields: boolPointer(true)},
					},
				}},
				Subresources: &apiextensionsv1.CustomResourceSubresources{Status: &apiextensionsv1.CustomResourceSubresourceStatus{}},
			}},
		},
	}
	if _, err := harness.Extensions.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create manifest adapter CRD: %v", err)
	}
	harness.track(func(ctx context.Context) error {
		err := harness.Extensions.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crd.Name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	})
	if err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		current, err := harness.Extensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crd.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("wait for manifest adapter CRD: %v", err)
	}
}

func installManifestAdapterNamespace(t *testing.T, ctx context.Context, harness *Harness) {
	t.Helper()
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   manifestAdapterNamespace,
		Labels: map[string]string{"environment": "integration"},
	}}
	if _, err := harness.Kubernetes.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create manifest adapter Namespace: %v", err)
	}
	harness.track(func(ctx context.Context) error {
		err := harness.Kubernetes.CoreV1().Namespaces().Delete(ctx, namespace.Name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	})
}

func installManifestAdapterConfiguration(t *testing.T, ctx context.Context, harness *Harness) *admissionregistrationv1.ValidatingWebhookConfiguration {
	t.Helper()
	url := "https://127.0.0.1:1/unused"
	failurePolicy := admissionregistrationv1.Fail
	matchPolicy := admissionregistrationv1.Exact
	sideEffects := admissionregistrationv1.SideEffectClassNone
	configuration := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: manifestAdapterConfiguration},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{{
			Name:         manifestAdapterWebhookName,
			ClientConfig: admissionregistrationv1.WebhookClientConfig{URL: &url},
			Rules: []admissionregistrationv1.RuleWithOperations{{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{"*"},
					APIVersions: []string{"*"},
					Resources:   []string{"*"},
				},
			}},
			FailurePolicy:           &failurePolicy,
			MatchPolicy:             &matchPolicy,
			NamespaceSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"environment": "integration"}},
			SideEffects:             &sideEffects,
			AdmissionReviewVersions: []string{"v1"},
		}},
	}
	created, err := harness.Kubernetes.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(ctx, configuration, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create manifest adapter WebhookConfiguration: %v", err)
	}
	harness.track(func(ctx context.Context) error {
		err := harness.Kubernetes.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(ctx, created.Name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	})
	return created
}

func connectManifestAdapterHydration(t *testing.T, ctx context.Context, harness *Harness) (*adapter.Hydration, *hydration.Reader) {
	t.Helper()
	path := writeManifestAdapterKubeconfig(t, harness, "")
	session, err := hydration.NewFactory().Connect(ctx, hydration.Options{Context: manifestAdapterContext, KubeconfigPath: path})
	if err != nil {
		t.Fatalf("hydration.Connect() error = %v", err)
	}
	profile := session.ProfileMatch()
	if profile.Status != manifest.ProfileMatchVerified || profile.ObservedKubernetesVersion != KubernetesVersion {
		t.Fatalf("hydration profile = %#v, want verified %s", profile, KubernetesVersion)
	}
	reader, err := session.NewReader()
	if err != nil {
		t.Fatalf("Session.NewReader() error = %v", err)
	}
	return &adapter.Hydration{Reader: reader, SourceLabel: session.ContextLabel(), ProfileMatch: profile}, reader
}

func writeManifestAdapterKubeconfig(t *testing.T, harness *Harness, impersonate string) string {
	t.Helper()
	config := clientcmdapi.NewConfig()
	config.Clusters[manifestAdapterContext] = &clientcmdapi.Cluster{
		Server:                   harness.Config.Host,
		CertificateAuthorityData: append([]byte(nil), harness.Config.CAData...),
	}
	config.AuthInfos[manifestAdapterContext] = &clientcmdapi.AuthInfo{
		ClientCertificateData: append([]byte(nil), harness.Config.CertData...),
		ClientKeyData:         append([]byte(nil), harness.Config.KeyData...),
		Token:                 harness.Config.BearerToken,
		Impersonate:           impersonate,
	}
	if impersonate != "" {
		config.AuthInfos[manifestAdapterContext].ImpersonateGroups = []string{"system:authenticated"}
	}
	config.Contexts[manifestAdapterContext] = &clientcmdapi.Context{Cluster: manifestAdapterContext, AuthInfo: manifestAdapterContext}
	config.CurrentContext = "must-not-be-used"
	data, err := clientcmd.Write(*config)
	if err != nil {
		t.Fatalf("clientcmd.Write() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("os.WriteFile(kubeconfig) error = %v", err)
	}
	return path
}

func assertManifestAdapterPermissionDenial(t *testing.T, ctx context.Context, harness *Harness) {
	t.Helper()
	path := writeManifestAdapterKubeconfig(t, harness, "manifest-adapter-no-access")
	session, err := hydration.NewFactory().Connect(ctx, hydration.Options{Context: manifestAdapterContext, KubeconfigPath: path})
	if err != nil {
		t.Fatalf("connect impersonated hydration session: %v", err)
	}
	reader, err := session.NewReader()
	if err != nil {
		t.Fatalf("create impersonated hydration reader: %v", err)
	}
	if result := reader.Discover(); result.Status != hydration.ReadStatusSuccess {
		t.Fatalf("unprivileged discovery status = %q, want success through system discovery role", result.Status)
	}
	if result := reader.ListValidatingConfigurations(ctx); result.Status != hydration.ReadStatusForbidden {
		t.Errorf("unprivileged validating LIST status = %q, want forbidden", result.Status)
	}
	if result := reader.ListMutatingConfigurations(ctx); result.Status != hydration.ReadStatusForbidden {
		t.Errorf("unprivileged mutating LIST status = %q, want forbidden", result.Status)
	}
	if result := reader.GetNamespace(ctx, manifestAdapterNamespace); result.Status != hydration.ReadStatusForbidden {
		t.Errorf("unprivileged Namespace GET status = %q, want forbidden", result.Status)
	}
}

func waitForManifestAdapterDiscovery(t *testing.T, ctx context.Context, reader *hydration.Reader) hydration.DiscoveryResult {
	t.Helper()
	var result hydration.DiscoveryResult
	crdGVK := schema.GroupVersionKind{Group: manifestAdapterCRDGroup, Version: "v1", Kind: manifestAdapterCRDKind}
	if err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(context.Context) (bool, error) {
		result = reader.Discover()
		if result.Status != hydration.ReadStatusSuccess {
			return false, result.Err
		}
		for _, resource := range result.Resources {
			if resource.GVK() == crdGVK {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("wait for CRD discovery: %v", err)
	}
	return result
}

func assertManifestAdapterCatalog(t *testing.T, discovery hydration.DiscoveryResult) {
	t.Helper()
	committedBytes, err := os.ReadFile(filepath.Join("..", "..", "internal", "compat", "kube136", "resource_catalog.json"))
	if err != nil {
		t.Fatalf("read committed resource catalog: %v", err)
	}
	committed, err := resourcecatalog.Parse(committedBytes, kube136.ProfileID, kube136.KubernetesVersion)
	if err != nil {
		t.Fatalf("resourcecatalog.Parse() error = %v", err)
	}
	builtIns := make([]resourcecatalog.Resource, 0, len(discovery.Resources))
	for _, resource := range discovery.Resources {
		if strings.Contains(resource.Resource, "/") {
			t.Errorf("verified discovery included subresource %#v", resource)
		}
		if resource.Group != manifestAdapterCRDGroup {
			builtIns = append(builtIns, resource)
		}
	}
	regenerated := resourcecatalog.Catalog{
		SchemaVersion:     resourcecatalog.SchemaVersion,
		ProfileID:         kube136.ProfileID,
		KubernetesVersion: kube136.KubernetesVersion,
		Resources:         builtIns,
	}
	if err := resourcecatalog.Compare(committed, regenerated); err != nil {
		t.Fatalf("pinned envtest catalog drift: %v", err)
	}
	regeneratedBytes, err := resourcecatalog.Marshal(regenerated)
	if err != nil {
		t.Fatalf("resourcecatalog.Marshal() error = %v", err)
	}
	if !bytes.Equal(regeneratedBytes, committedBytes) {
		t.Error("pinned envtest catalog bytes differ from committed canonical catalog")
	}
}

func assertManifestAdapterCRDResolution(t *testing.T, discovery hydration.DiscoveryResult) {
	t.Helper()
	gvk := schema.GroupVersionKind{Group: manifestAdapterCRDGroup, Version: "v1", Kind: manifestAdapterCRDKind}
	if _, ok := kube136.LookupResource(gvk); ok {
		t.Fatalf("dynamic CRD %s leaked into embedded built-in catalog", gvk)
	}
	resolver, err := discovery.Resolver("context:" + manifestAdapterContext)
	if err != nil {
		t.Fatalf("DiscoveryResult.Resolver() error = %v", err)
	}
	resolved, err := resolver.Resolve(gvk)
	if err != nil {
		t.Fatalf("Resolve(CRD) error = %v", err)
	}
	wantGVR := schema.GroupVersionResource{Group: manifestAdapterCRDGroup, Version: "v1", Resource: manifestAdapterCRDResource}
	if resolved.GVR != wantGVR || !resolved.Namespaced || resolved.Source != manifest.ResolutionSourceVerifiedDiscovery {
		t.Errorf("CRD resolution = %#v, want exact namespaced %s", resolved, wantGVR)
	}
}

func decodeManifestAdapterResources(t *testing.T) []manifest.Document {
	t.Helper()
	input := `apiVersion: v1
kind: Pod
metadata: {name: built-in, namespace: ` + manifestAdapterNamespace + `}
spec:
  containers: [{name: app, image: example.invalid/app:v1}]
---
apiVersion: ` + manifestAdapterCRDGroup + `/v1
kind: ` + manifestAdapterCRDKind + `
metadata: {name: custom, namespace: ` + manifestAdapterNamespace + `}
spec:
  secretLike: preserve-exactly
  unknown: {nested: [one, two]}
`
	decoded, err := manifest.Decode(strings.NewReader(input), manifest.SourceKindFile, "resources.yaml")
	if err != nil {
		t.Fatalf("manifest.Decode() error = %v", err)
	}
	return decoded.Documents
}

func assertManifestAdapterHydratedResults(t *testing.T, ctx context.Context, resolved adapter.Result) {
	t.Helper()
	if resolved.ProfileMatch.Status != manifest.ProfileMatchVerified || len(resolved.BuiltScenarios) != 2 || len(resolved.Resources) != 2 {
		t.Fatalf("hydrated adapter result shape = %#v", resolved)
	}
	for index, resource := range resolved.Resources {
		completeness := resource.Completeness
		if completeness.Configuration.Status != manifest.CompletenessHydrated ||
			completeness.Discovery.Status != manifest.CompletenessHydrated ||
			completeness.Namespace.Status != manifest.CompletenessHydrated {
			t.Errorf("resource %d completeness = %#v, want hydrated configuration/discovery/Namespace", index+1, completeness)
		}
		built := resolved.BuiltScenarios[index]
		if built.Scenario.ExternalContext == nil || built.Scenario.ExternalContext.Namespace == nil ||
			built.Scenario.ExternalContext.Namespace.Labels["environment"] != "integration" {
			t.Errorf("resource %d Namespace fixture = %#v", index+1, built.Scenario.ExternalContext)
		}
		if !reflect.DeepEqual(built.Scenario.Request.UserInfo, zeroUserInfo()) {
			t.Errorf("resource %d inherited connection identity: %#v", index+1, built.Scenario.Request.UserInfo)
		}
		evaluated, err := manifest.EvaluateBuiltScenario(ctx, built)
		if err != nil {
			t.Fatalf("EvaluateBuiltScenario(%d) error = %v", index+1, err)
		}
		if len(evaluated.Result.Webhooks) != 1 || evaluated.Result.Webhooks[0].Outcome == nil || *evaluated.Result.Webhooks[0].Outcome != contract.OutcomeCalled {
			t.Errorf("resource %d namespaceSelector evaluation = %#v, want called", index+1, evaluated.Result.Webhooks)
		}
	}
}

func assertManifestAdapterSnapshotReplay(t *testing.T, ctx context.Context, harness *Harness, built []manifest.BuiltScenario) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "snapshots")
	if err := snapshot.NewWriter().Write(target, built); err != nil {
		t.Fatalf("snapshot.Write() error = %v", err)
	}
	for _, item := range built {
		path := filepath.Join(target, item.SnapshotName)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("os.ReadFile(%s) error = %v", item.SnapshotName, err)
		}
		for _, forbidden := range []string{harness.Config.Host, manifestAdapterContext} {
			if forbidden != "" && bytes.Contains(data, []byte(forbidden)) {
				t.Errorf("snapshot %s contains connection metadata %q", item.SnapshotName, forbidden)
			}
		}
		if item.Resolution.GVK.Group == manifestAdapterCRDGroup && !bytes.Contains(data, []byte("preserve-exactly")) {
			t.Errorf("CRD snapshot %s lost exact user payload", item.SnapshotName)
		}

		replayedScenario, err := scenario.Decode(data)
		if err != nil {
			t.Fatalf("scenario.Decode(%s) error = %v", item.SnapshotName, err)
		}
		if !reflect.DeepEqual(replayedScenario.Request.UserInfo, zeroUserInfo()) {
			t.Errorf("snapshot %s contains automatic connection identity: %#v", item.SnapshotName, replayedScenario.Request.UserInfo)
		}
		original, err := manifest.EvaluateBuiltScenario(ctx, item)
		if err != nil {
			t.Fatalf("EvaluateBuiltScenario(%s) error = %v", item.SnapshotName, err)
		}
		replayInput, err := evaluation.SnapshotFromScenario(*replayedScenario)
		if err != nil {
			t.Fatalf("evaluation.SnapshotFromScenario(%s) error = %v", item.SnapshotName, err)
		}
		replayed := evaluation.NewEvaluator().Evaluate(ctx, replayInput)
		originalJSON, err := render.JSON(original.Result)
		if err != nil {
			t.Fatalf("render.JSON(original %s) error = %v", item.SnapshotName, err)
		}
		replayedJSON, err := render.JSON(replayed)
		if err != nil {
			t.Fatalf("render.JSON(replayed %s) error = %v", item.SnapshotName, err)
		}
		if !bytes.Equal(replayedJSON, originalJSON) {
			t.Errorf("snapshot %s canonical evaluator result drifted", item.SnapshotName)
		}
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func zeroUserInfo() authenticationv1.UserInfo {
	return authenticationv1.UserInfo{}
}
