# Test environment setup

English | [한국어](test-environment-setup.ko.md)

This guide prepares three levels of validation for an AdmiTrace change:

1. Offline regression checks that require no cluster
2. Pinned envtest parity and conformance against the exact Kubernetes `1.36.2` API server
3. Optional Docker and kind end-to-end checks that include real Webhook transport

> [!IMPORTANT]
> In AdmiTrace, `called` means the supported routing pipeline selected a Webhook for
> invocation. It does not mean an HTTP/TLS request was sent, or that a Webhook response,
> allow/deny decision, patch, or reinvocation was observed.

## Choose the right environment

| Goal | Recommended environment | Real API server | Real Webhook call |
| --- | --- | --- | --- |
| Fast development regression | Local Go tests and standalone CLI | No | No |
| Release parity and conformance | Kubernetes `1.36.2` envtest | Yes | Test-only recorder |
| Deployment-like end-to-end | Docker + kind `1.36.2` | Yes | Yes, after deploying a Webhook |

Use envtest first to reproduce the repository's release evidence. Add Docker and kind when
you need to deploy a real Webhook server, TLS, Service, and `WebhookConfiguration` and
observe transport. Do not use Docker Desktop's built-in Kubernetes as parity evidence: its
exact patch version is not straightforward to pin for this profile.

## Common prerequisites

From the repository root, verify Go:

```sh
go version
```

The required version is:

```text
go version go1.26.5 ...
```

Run the offline baseline first:

```sh
go test -count=1 ./...
go vet ./...
./hack/verify-dependencies.sh
```

Build a fresh standalone CLI:

```sh
mkdir -p ./build
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace version
./build/admitrace test docs/examples
./build/admitrace test validation/beta/scenarios
```

Every command must return exit code `0` before proceeding.

## envtest: recommended parity and conformance environment

envtest runs `kube-apiserver` and `etcd` locally. It observes real Kubernetes API server
defaulting, validation, discovery, RBAC, and admission routing without requiring a Docker
cluster. The AdmiTrace conformance harness pins:

- Kubernetes control-plane assets `1.36.2`
- `sigs.k8s.io/controller-runtime` `v0.24.1`
- Kubernetes Go modules `v0.36.2`

### 1. Install setup-envtest

Install the pinned tool from a network-enabled environment:

```sh
go install sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.24.1
```

### 2. Provision Kubernetes 1.36.2 assets

The repository ignores `.tools/`. Download the assets there and export the absolute path
reported by setup-envtest:

```sh
mkdir -p ./.tools/envtest
ENVTEST_TOOL="$(go env GOPATH)/bin/setup-envtest"
KUBEBUILDER_ASSETS="$("$ENVTEST_TOOL" use 1.36.2 --bin-dir ./.tools/envtest -p path)"
export KUBEBUILDER_ASSETS
```

If exact `1.36.2` assets are unavailable, stop instead of substituting another patch
version. Another version is not parity evidence for the `kubernetes-1.36.2-defaults`
profile.

### 3. Verify the assets

All three executables must exist:

```sh
test -x "$KUBEBUILDER_ASSETS/kube-apiserver"
test -x "$KUBEBUILDER_ASSETS/etcd"
test -x "$KUBEBUILDER_ASSETS/kubectl"
"$KUBEBUILDER_ASSETS/kube-apiserver" --version
```

The final command must report exactly:

```text
Kubernetes v1.36.2
```

The conformance commands never download dependencies. Pre-populate both the root module
and nested conformance module caches:

```sh
go mod download
go -C conformance mod download
```

### 4. Run staged validation

Run the full conformance suite:

```sh
./hack/test-conformance.sh
```

Validate the public Gatekeeper and Istio cases against both fresh CLI output and the
Kubernetes oracle:

```sh
./hack/test-beta-validation.sh
```

Optionally preserve a deterministic parity report:

```sh
PARITY_REPORT=/tmp/admitrace-parity.json ./hack/test-parity-gate.sh
```

### 5. Run the complete release gate

Finally run root tests, vet, dependency boundaries, fuzz, standalone smoke, parity,
conformance, and beta validation together:

```sh
GO_BIN="$(command -v go)" ./hack/verify-release-readiness.sh
```

Success ends with:

```text
release readiness: passed
```

## Docker + kind: optional end-to-end environment

kind runs Kubernetes nodes as Docker containers. Use it to submit a real
`kubectl --dry-run=server` request and compare AdmiTrace with Webhook Pod logs.

> [!WARNING]
> Creating a kind cluster alone does not test Webhook transport. You must separately
> deploy a Webhook server, TLS certificate, Service, and valid Validating or
> MutatingWebhookConfiguration inside the cluster.

### 1. Prepare Docker, kind, and kubectl

On macOS, start Docker Desktop first. Verify that both the Docker client and server answer:

```sh
docker version
```

Install kind and kubectl if needed:

```sh
brew install kind kubectl
kind version
kubectl version --client
```

When building a Kubernetes node image on macOS or Windows, allocate at least 6 GiB and
preferably 8 GiB of memory to the Docker VM.

### 2. Build the exact 1.36.2 node image

The kind `v0.32.0` release ships a prebuilt Kubernetes `1.36.1` image. AdmiTrace rejects
`1.36.1` for live hydration, so build an image from the upstream `v1.36.2` release:

```sh
kind build node-image \
  --type release \
  --image admitrace/kindest-node:v1.36.2 \
  v1.36.2
```

Inspect the result:

```sh
docker image inspect admitrace/kindest-node:v1.36.2
```

If an official prebuilt `v1.36.2` image becomes available later, pin the digest published
in that kind release note instead of relying on the tag alone.

### 3. Create a dedicated cluster

Use a dedicated name to avoid confusing it with an operations context:

```sh
kind create cluster \
  --name admitrace-e2e \
  --image admitrace/kindest-node:v1.36.2
```

Write a separate kubeconfig and pass it explicitly to every command:

```sh
mkdir -p ./.tools
kind get kubeconfig --name admitrace-e2e > ./.tools/kind-admitrace-e2e.kubeconfig
kubectl \
  --kubeconfig "$PWD/.tools/kind-admitrace-e2e.kubeconfig" \
  --context kind-admitrace-e2e \
  version
```

Confirm that Server Version is exactly `v1.36.2`.

### 4. Deploy the real Webhook

The Webhook under test needs at least:

- A Webhook server Deployment or Pod
- A Kubernetes Service reachable by the API server
- A TLS server certificate matching the Service DNS name
- A `clientConfig.caBundle` that verifies that certificate
- `sideEffects: None` or dry-run-aware `NoneOnDryRun`
- Supported `admissionReviewVersions`
- Rules, selectors, and `matchConditions` for the intended case

`docs/manifest-examples/webhooks.yaml` is an offline adapter example. It does not provide a
real Webhook server or complete transport configuration, so do not apply it unchanged as an
end-to-end transport fixture.

Check readiness and registered configurations:

```sh
kubectl \
  --kubeconfig "$PWD/.tools/kind-admitrace-e2e.kubeconfig" \
  --context kind-admitrace-e2e \
  get validatingwebhookconfigurations,mutatingwebhookconfigurations
```

### 5. Run AdmiTrace live hydration

Prepare a resource YAML that will not be created, then use an explicit context and
kubeconfig:

```sh
./build/admitrace --output json explain \
  --resource /absolute/path/to/resource.yaml \
  --context kind-admitrace-e2e \
  --kubeconfig "$PWD/.tools/kind-admitrace-e2e.kubeconfig" \
  > /tmp/admitrace-kind-result.json
```

AdmiTrace hydration performs only version, discovery, WebhookConfiguration LIST, and the
needed Namespace GET requests. Inspect `called` or `skipped`, the terminal `reasonCode`, and
`contextCompleteness`.

### 6. Compare the real API server result

Submit the same resource with server-side dry-run:

```sh
kubectl \
  --kubeconfig "$PWD/.tools/kind-admitrace-e2e.kubeconfig" \
  --context kind-admitrace-e2e \
  apply --dry-run=server \
  -f /absolute/path/to/resource.yaml
```

A dry-run request can still perform real Webhook transport. Use only a test cluster and
confirm that the Webhook declares `sideEffects: None` or `NoneOnDryRun` first.

Inspect the Webhook logs:

```sh
kubectl \
  --kubeconfig "$PWD/.tools/kind-admitrace-e2e.kubeconfig" \
  --context kind-admitrace-e2e \
  logs -n WEBHOOK_NAMESPACE deploy/WEBHOOK_DEPLOYMENT
```

Compare observations as follows:

| AdmiTrace | API server and Webhook observation |
| --- | --- |
| `called` | An AdmissionReview request reaches that Webhook |
| `skipped` | No request reaches that Webhook |
| `rejected-before-call` | The request is rejected before Webhook transport |
| `indeterminate` | Missing Namespace, identity, or fixture context can be supplied |

Even when a Webhook receives a request, inspect the API server response and Webhook logs
separately for allow/deny, patching, and reinvocation. Those are not AdmiTrace results.

### 7. Delete the cluster

Delete only the dedicated test cluster when finished:

```sh
kind delete cluster --name admitrace-e2e
```

`.tools/kind-admitrace-e2e.kubeconfig` then points only to the deleted cluster, and `.tools/`
is ignored by Git.

## Using an existing external cluster

If you use an existing cluster instead of kind, select a dedicated test cluster rather than
production. Verify the server version first:

```sh
kubectl \
  --kubeconfig /absolute/path/to/kubeconfig \
  --context TEST_CONTEXT \
  version
```

Do not run live hydration when the server is not exactly `v1.36.2`. Always pass the context
name and kubeconfig path explicitly to both AdmiTrace and kubectl.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| `Cannot connect to the Docker daemon` | Docker Desktop is running and `docker version` includes Server output |
| Server version mismatch | The cluster is exactly Kubernetes `v1.36.2` |
| `KUBEBUILDER_ASSETS` error | Executable `kube-apiserver`, `etcd`, and `kubectl` assets and their versions |
| Dry-run rejected before the Webhook | `sideEffects` is `None` or `NoneOnDryRun` |
| AdmiTrace says `called`, but no Webhook log appears | Service DNS, endpoints, TLS, CA bundle, timeout, and AdmissionReview negotiation |
| Offline CRD is `unsupported` | An explicit exact-version context is available for discovery |

When `called` differs from the transport observation, inspect Service, TLS, and API server
events as well as the selector, rule, and CEL trace. Transport is outside AdmiTrace's scope,
so a difference does not by itself prove a routing evaluator defect.

## Upstream references

- [kind Quick Start](https://kind.sigs.k8s.io/docs/user/quick-start/)
- [kind releases](https://github.com/kubernetes-sigs/kind/releases)
- [Kubernetes v1.36.2 release](https://github.com/kubernetes/kubernetes/releases/tag/v1.36.2)
- [Kubebuilder envtest configuration](https://book.kubebuilder.io/reference/envtest.html)
- [Kubernetes dynamic admission control](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
