# AdmiTrace

English | [한국어](README.ko.md)

AdmiTrace reproduces Kubernetes Admission Webhook routing decisions offline and explains them as an ordered trace.

It evaluates `ValidatingWebhookConfiguration` and `MutatingWebhookConfiguration` objects against the same request snapshot. The first compatibility profile targets the default behavior of Kubernetes `1.36.2`.

> [!IMPORTANT]
> In AdmiTrace, `called` means that a Webhook was **selected for invocation** by the supported routing pipeline. It does not mean that an HTTP/TLS request was sent or that the Webhook returned a successful response.

## Installation

Install the tagged release with Go:

```bash
go install github.com/silbaram/admitrace/cmd/admitrace@v0.1.2
```

Make sure `$(go env GOPATH)/bin` (or `GOBIN`) is on `PATH`, then verify the installation:

```bash
admitrace version
admitrace --help
```

Release source: [`v0.1.2`](https://github.com/silbaram/admitrace/tree/v0.1.2)

## Documentation

- [Quickstart](docs/quickstart.md): build the CLI, run the validating and mutating examples, and add expectation checks to CI.
- [Scenario and result reference](docs/reference.md): schemas, reason codes, exit codes, support policy, and explicit non-goals.
- [Public beta validation](validation/beta/README.md): pinned Gatekeeper and Istio cases with license provenance, CLI results, and Kubernetes `1.36.2` oracle evidence.
- [v0.1 release readiness](docs/release-readiness.md): one fail-closed command for pins, unit and fuzz tests, standalone smoke, parity, conformance, beta evidence, and documentation checks.

## Project status

The repository implements the offline decision core, canonical text and JSON rendering, the `version`, `explain`, and `test` commands, safety guardrails, and a Kubernetes `1.36.2` envtest parity gate.

AdmiTrace is not a live-cluster operations tool. The current release scope prioritizes semantic accuracy, explainability, and reproducible parity over transport or adapter integrations.

## Core capabilities

- Strict YAML/JSON Scenario decoding and input validation
- Common normalization for validating and mutating Webhooks
- Namespace selector and object selector evaluation
- Exact rule matching by operation, GVR, subresource, and scope
- Equivalent fallback backed by explicit fixtures
- `matchConditions` evaluation using the Kubernetes CEL environment
- `failurePolicy` Fail/Ignore behavior with false-result precedence
- Fixture-backed Namespace, authorization, and equivalence context
- Source-addressable traces with stable reason codes and pending, discarded, and terminal states
- Ordered, independent evaluation of multiple Webhooks against one snapshot
- Offline runtime without live Kubernetes clients or outbound network access

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
- Webhook kinds:
  - `ValidatingWebhookConfiguration`
  - `MutatingWebhookConfiguration`

`matchPolicy=Exact` is evaluated directly within the supported profile. `Equivalent` continues only after an Exact miss and only when an explicit equivalence fixture is available. Namespace and authorization context are also supplied exclusively through fixtures; no live cluster lookup is performed.

## Out of scope

AdmiTrace does not currently perform the following operations:

- Sending actual Admission Webhook HTTP/TLS requests
- Validating Webhook response allow/deny decisions, status, or audit annotations
- Applying JSON Patch output or predicting a mutating patch chain
- Simulating `reinvocationPolicy`
- Querying live Namespace, authorization, or API discovery state
- Negotiating `AdmissionReviewVersions` or guaranteeing transport success
- Simulating kube-apiserver dry-run pre-call rejection; unsupported `dryRun` and `sideEffects` combinations remain `unsupported`
- Capturing a live request snapshot from a cluster
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

## License

AdmiTrace is licensed under the Apache License 2.0.
