# AdmiTrace

English | [한국어](README.ko.md)

AdmiTrace reproduces Kubernetes Admission Webhook routing decisions and explains them as an ordered trace. It accepts replayable Scenarios or ordinary Kubernetes resource YAML; resource mode is offline by default with explicit, GET-only context hydration available when needed.

It evaluates `ValidatingWebhookConfiguration` and `MutatingWebhookConfiguration` objects against the same request snapshot. The first compatibility profile targets the default behavior of Kubernetes `1.36.2`.

> [!IMPORTANT]
> In AdmiTrace, `called` means that a Webhook was **selected for invocation** by the supported routing pipeline. It does not mean that an HTTP/TLS request was sent or that the Webhook returned a successful response.

## Installation

Install the tagged release with Go:

```bash
go install github.com/silbaram/admitrace/cmd/admitrace@v0.1.0
```

Make sure `$(go env GOPATH)/bin` (or `GOBIN`) is on `PATH`, then verify the installation:

```bash
admitrace version
admitrace --help
```

Release source: [`v0.1.0`](https://github.com/silbaram/admitrace/tree/v0.1.0)

## Documentation

- [Quickstart](docs/quickstart.md): run single/multi-document resources offline, opt into limited hydration, export snapshots, and preserve Scenario CI checks.
- [Scenario, manifest adapter, and result reference](docs/reference.md): schemas, provenance, GET-only hydration, SnapshotPolicy, reason codes, exit codes, and explicit non-goals.
- [Public beta validation](validation/beta/README.md): pinned Gatekeeper and Istio cases with license provenance, CLI results, and Kubernetes `1.36.2` oracle evidence.
- [v0.1 release readiness](docs/release-readiness.md): one fail-closed command for pins, unit and fuzz tests, standalone smoke, parity, conformance, beta evidence, and documentation checks.

## Project status

The repository implements the offline decision core, a universal manifest adapter, a generated Kubernetes `1.36.2` built-in resource catalog, opt-in GET-only hydration, exact-copy Scenario snapshots, canonical text/JSON rendering, and a pinned envtest parity gate.

AdmiTrace is not a live-cluster operations tool. Hydration reads only the narrow context needed for one explicit explanation; it does not audit or observe a cluster continuously.

## Core capabilities

- Strict YAML/JSON Scenario decoding and input validation
- Universal `-f/--file` detection for legacy Scenarios and raw Kubernetes resources
- Deterministic single/multi-document, stdin, and resource-directory adaptation with 1-based provenance
- Exact offline GVK→GVR/scope resolution from the generated `1.36.2` built-in catalog
- Explicit-context hydration limited to version/discovery, WebhookConfiguration LIST, and needed Namespace GET requests
- File-first configuration/Namespace fallbacks and explicit admission identity flags
- Exact-copy-or-refuse Scenario snapshot export and offline replay
- Common normalization for validating and mutating Webhooks
- Namespace selector and object selector evaluation
- Exact rule matching by operation, GVR, subresource, and scope
- Equivalent fallback backed by explicit fixtures
- `matchConditions` evaluation using the Kubernetes CEL environment
- `failurePolicy` Fail/Ignore behavior with false-result precedence
- Fixture-backed Namespace, authorization, and equivalence context
- Source-addressable traces with stable reason codes and pending, discarded, and terminal states
- Ordered, independent evaluation of multiple Webhooks against one snapshot
- Zero client construction and network access unless `--context` is explicitly selected

## Decision model

AdmiTrace separates `determination` from the optional `outcome`.

| Determination | Meaning | Outcome |
| --- | --- | --- |
| `determinate` | Evaluation completed within the supported contract | Required |
| `indeterminate` | Required fixture context was not supplied | Absent |
| `unsupported` | Evaluation requires semantics outside the compatibility profile | Absent |

Only a `determinate` result has one of the following outcomes.

| Outcome | Meaning |
| --- | --- |
| `called` | Selected by the snapshot routing pipeline |
| `skipped` | Excluded by a selector, rule, or CEL condition |
| `rejected-before-call` | A pre-invocation evaluation error resulted in request rejection |

Validating evaluations use the `snapshot-routing` phase. Mutating evaluations use `mutating-initial-snapshot-eligibility`, meaning eligibility before any Webhook patch or reinvocation could alter the request.

## Supported scope

- Scenario API: `admitrace.io/v1alpha1`
- Result schema: `admitrace.result/v1alpha1`
- Kubernetes profile: `kubernetes-1.36.2-defaults`
- Kubernetes modules: `v0.36.2`
- Configuration API: `admissionregistration.k8s.io/v1`
- Raw-resource operation: `CREATE` only
- Resource explanation schema: `admitrace.manifest-explanation/v1alpha1`
- Hydration boundary: explicit context, exact server `v1.36.2`, HTTP GET only
- Webhook kinds:
  - `ValidatingWebhookConfiguration`
  - `MutatingWebhookConfiguration`

`-f/--file` is universal: one `admitrace.io/v1alpha1` Scenario keeps legacy output; other file/stdin/directory inputs use resource mode. Offline resources must exist in the generated built-in catalog. CRDs require verified discovery from an explicit exact-version context and are never pluralized heuristically.

Hydration never infers a context from current kubeconfig state. It permits only GET requests for version, discovery, WebhookConfiguration LIST, and a needed Namespace. Explicit `--webhook-config` and `--namespace-object` files suppress the corresponding cluster reads. Kubeconfig identity is not admission identity; only `--user`, `--group`, `--user-uid`, and `--user-extra` populate `request.userInfo`.

## Out of scope

AdmiTrace does not currently perform the following operations:

- Sending actual Admission Webhook HTTP/TLS requests
- Validating Webhook response allow/deny decisions, status, or audit annotations
- Applying JSON Patch output or predicting a mutating patch chain
- Simulating `reinvocationPolicy`
- Implicit current-context access, cluster-wide audit, watch/informer use, or API mutation
- SubjectAccessReview or other POST-based permission preflight
- Negotiating `AdmissionReviewVersions` or guaranteeing transport success
- Simulating kube-apiserver dry-run pre-call rejection; unsupported `dryRun` and `sideEffects` combinations remain `unsupported`
- Capturing AdmissionRequest history or kubeconfig credentials as request identity
- Testing timeouts, certificates, network failures, performance, or load
- Approximating behavior for unverified Kubernetes versions
- Providing a stable public Go API, JUnit XML, or project-specific adapters

## Development environment

- Go `1.26.0`
- Go toolchain `1.26.5`
- Kubernetes staging modules `v0.36.2`
- Cobra `v1.10.2`

Build and test the repository with:

```bash
git clone https://github.com/silbaram/admitrace.git
cd admitrace

go test ./...
go vet ./...
mkdir -p ./build
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace --help
./build/admitrace explain --file docs/examples/validating.yaml
./build/admitrace explain --resource docs/manifest-examples/resource.yaml \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml
./build/admitrace --output json test docs/examples
```

Verify the Kubernetes dependency boundary with:

```bash
./hack/verify-dependencies.sh
```

### Local safety limits

AdmiTrace rejects a Scenario larger than 1 MiB or nested deeper than 100
containers. One `admitrace test` invocation accepts at most 1,000 discovered
Scenario documents. Limit failures are stable invalid-input diagnostics; they
are never interpreted through a Webhook `failurePolicy`.

The guardrails are split into two independently runnable groups:

```bash
./hack/test-resource-limits-fuzz.sh
./hack/test-redaction-offline-determinism.sh
```

The first group exercises input/document/CEL limits and seeded decoder and
fixture fuzz targets. The second verifies sensitive-value redaction, canonical
byte equality, offline execution, and the production runtime boundary.

## Architecture

```text
cmd/admitrace             CLI entrypoint
internal/cli              Cobra command boundary
internal/scenario         Scenario decoding, defaults, and validation
internal/normalize        Webhook and request normalization
internal/fixture          Namespace, equivalence, and authorization fixtures
internal/compat/kube136   Kubernetes 1.36 compatibility adapters
internal/resourcecatalog  Generated built-in discovery catalog contract
internal/manifest         Raw manifest decoding, identity, and Scenario builder
internal/adapter          File-first context completeness and hydration resolver
internal/hydration        Explicit exact-version GET-only Kubernetes reader
internal/snapshot         Exact-copy-or-refuse Scenario bundle writer
internal/routing          Selectors, rules, and pre-CEL orchestration
internal/matchcondition   CEL matchConditions evaluation
internal/evaluation       Integrated snapshot evaluator
internal/contract         Scenario, result, trace, and diagnostic contracts
```

Kubernetes-version-specific code is isolated behind `internal/compat/kube136`. The decision core does not access the network or terminate the process; the CLI owns input/output and exit-code policy.

## Development principles

- Prioritize Kubernetes parity and explainability over broad feature coverage.
- Never guess when required external context is missing.
- Keep `indeterminate`, `unsupported`, Kubernetes evaluation errors, and invalid input distinct.
- Preserve input order and deterministic output in traces and canonical results.
- Do not copy Secret payloads or authorization identities into diagnostics.
- Do not use the `k8s.io/kubernetes` root module as a production dependency.

## Version policy

The profile `kubernetes-1.36.2-defaults` means exactly Kubernetes `1.36.2` with that release's default feature gates. A new Kubernetes version requires a separate compatibility profile and exact-version parity evidence; it is never routed through the existing profile by approximation.
