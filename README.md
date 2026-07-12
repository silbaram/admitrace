# AdmiTrace

English | [한국어](README.ko.md)

AdmiTrace reproduces Kubernetes Admission Webhook routing decisions offline and explains them as an ordered trace.

It evaluates `ValidatingWebhookConfiguration` and `MutatingWebhookConfiguration` objects against the same request snapshot. The first compatibility profile targets the default behavior of Kubernetes `1.36.2`.

> [!IMPORTANT]
> In AdmiTrace, `called` means that a Webhook was **selected for invocation** by the supported routing pipeline. It does not mean that an HTTP/TLS request was sent or that the Webhook returned a successful response.

## Project status

The repository currently implements the decision core from the Scenario contract through the integrated snapshot evaluator. The canonical renderer, user-facing `version`, `explain`, and `test` commands, and the Kubernetes envtest parity suite are still under development.

AdmiTrace is therefore not yet a finished CLI product or a live-cluster operations tool. The project currently prioritizes semantic accuracy, explainability, and parity verification.

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
- Testing timeouts, certificates, network failures, performance, or load
- Approximating behavior for unverified Kubernetes versions

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
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace --help
```

Verify the Kubernetes dependency boundary with:

```bash
./hack/verify-dependencies.sh
```

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

## Roadmap

- Canonical JSON and text renderers
- `version`, `explain`, and `test` CLI commands
- Input limits, redaction, fuzzing, and offline guardrails
- Kubernetes `1.36.2` envtest oracle and parity matrix
- User documentation, real-world Webhook validation, and v0.1 release readiness

