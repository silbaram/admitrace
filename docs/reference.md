# Scenario and result reference

English | [한국어](reference.ko.md)

This reference describes the public contract implemented by the current CLI. The only supported compatibility target is Kubernetes `1.36.2` with that release's default feature-gate policy.

## Scenario envelope

A Scenario is strict YAML or JSON with these top-level fields:

| Field | Required | Contract |
| --- | --- | --- |
| `apiVersion` | yes | Exactly `admitrace.io/v1alpha1` |
| `kind` | yes | Exactly `Scenario` |
| `metadata.name` | yes | Stable Scenario identifier |
| `compatibilityProfile` | yes | Exact supported profile described below |
| `configuration` | yes | Exactly one of `validatingWebhookConfiguration` or `mutatingWebhookConfiguration` |
| `request` | yes | Immutable AdmissionRequest snapshot used by every configured Webhook |
| `externalContext` | no | Fixture-backed Namespace, authorization decisions, and equivalence mappings |
| `expectations` | no | Per-Webhook assertions consumed by `admitrace test` |

Unknown fields and duplicate YAML or JSON keys are rejected. Documents are limited to 1 MiB and 100 nested containers.

### Compatibility profile

Every current Scenario must use exactly:

```yaml
compatibilityProfile:
  id: kubernetes-1.36.2-defaults
  kubernetesVersion: 1.36.2
  featureGatePolicy: kubernetes-defaults
```

This means Kubernetes `1.36.2` behavior with the upstream default feature-gate values for that release. It is not an LTS range, minimum version, or best-effort alias for other Kubernetes releases.

A future Kubernetes version is added only as a new, isolated compatibility implementation and profile after its fixtures pass an exact-version API server parity matrix. Existing profiles keep their original meaning. AdmiTrace never silently evaluates a new version through the `1.36.2` profile.

### Configuration

`configuration.validatingWebhookConfiguration` and `configuration.mutatingWebhookConfiguration` use the `admissionregistration.k8s.io/v1` API shape. The evaluator consumes routing fields including:

- ordered `webhooks`
- `rules` operations, API groups, API versions, resources, subresources, and scope
- `namespaceSelector` and `objectSelector`
- `matchPolicy` (`Exact` or `Equivalent`)
- `matchConditions`
- `failurePolicy` (`Fail` or `Ignore`)
- `sideEffects` only for the bounded dry-run compatibility classification described below

Kubernetes routing defaults used by this profile are applied when the corresponding fields are omitted: `failurePolicy: Fail`, `matchPolicy: Equivalent`, and rule `scope: "*"`.

When `request.dryRun` is true, `sideEffects: None` and `NoneOnDryRun` can remain eligible. Other supplied `sideEffects` values produce `determination: unsupported` with `CAPABILITY_OUTSIDE_PROFILE`. AdmiTrace does not simulate kube-apiserver's dry-run pre-call rejection or transport behavior.

### Request snapshot

`request` provides the routing input. Important fields are:

| Field | Meaning |
| --- | --- |
| `kind`, `resource`, `subResource` | Effective GVK, GVR, and subresource |
| `requestKind`, `requestResource`, `requestSubResource` | Original request identity when conversion supplied a different effective identity |
| `operation` | `CREATE`, `UPDATE`, `DELETE`, or `CONNECT` |
| `scope` | `Namespaced` or `Cluster` |
| `name`, `namespace`, `userInfo`, `dryRun` | Admission attributes used by routing or CEL |
| `object`, `oldObject`, `options` | Raw JSON-compatible snapshots; absent and explicit `null` remain distinct inputs |

Every Webhook is evaluated independently against this same immutable snapshot and in configuration order.

### External context fixtures

AdmiTrace does not query a live cluster. Supply context under `externalContext` when routing needs it:

- `namespace`: the exact Namespace object used by a non-empty namespace selector for a namespaced request.
- `authorization`: exact resource or non-resource authorizer queries with `allow`, `deny`, `no-opinion`, or `error` verdicts.
- `equivalence`: the exact request GVR/subresource mapped to ordered equivalent GVR, GVK, and subresource candidates.

An absent required fixture produces `indeterminate`; it does not trigger a live lookup. A supplied transformation that the profile cannot represent produces `unsupported`; it is not guessed.

### Expectations

Each expectation identifies a configured `webhookName` and requires `determination`. `outcome` and `terminalReasonCode` are optional assertions.

```yaml
expectations:
  - webhookName: pods.example.admitrace.io
    determination: determinate
    outcome: called
    terminalReasonCode: MATCH_CONDITIONS_TRUE
```

For an incomplete expectation, omit `outcome`:

```yaml
expectations:
  - webhookName: pods.example.admitrace.io
    determination: indeterminate
    terminalReasonCode: NAMESPACE_CONTEXT_MISSING
```

## Result schema

`admitrace explain --output json` emits `admitrace.result/v1alpha1`:

| Field | Meaning |
| --- | --- |
| `schemaVersion` | Exactly `admitrace.result/v1alpha1` |
| `scenarioId` | Source `metadata.name` |
| `compatibilityProfile` | Exact profile used for evaluation |
| `evaluationPhase` | `snapshot-routing` or `mutating-initial-snapshot-eligibility` |
| `configurationKind` | Validating or Mutating Webhook configuration kind |
| `webhooks` | Ordered independent Webhook results |
| `diagnostics` | Result-level structured diagnostics |

Each `webhooks[]` entry contains `webhookName`, `webhookIndex`, `sourcePath`, `determination`, optional `outcome`, ordered `trace`, and `diagnostics`.

### Determination and outcome

| Determination | Outcome | Meaning |
| --- | --- | --- |
| `determinate` | required | Evaluation completed inside the supported contract |
| `indeterminate` | absent | Required fixture context is missing; the tool refuses to guess |
| `unsupported` | absent | Required semantics are outside this compatibility profile |

Determinate outcomes are:

| Outcome | Meaning |
| --- | --- |
| `called` | Selected for invocation by the supported snapshot routing pipeline |
| `skipped` | Excluded by a selector, rule, or match condition |
| `rejected-before-call` | A controlling evaluation error and `failurePolicy` reject before invocation |

`called` is not evidence of an HTTP request or an allowed Webhook response.

### Trace

Every trace step includes:

- `stage` and exact `sourcePath`
- zero-based `sequence`
- redacted `inputSummary`
- `result` and stable `reasonCode`
- `pending`, `discarded`, and `terminal` state

A selector problem may remain `pending` until rule applicability is known. It becomes `discarded` if a later no-match makes it irrelevant, or a separate terminal problem step if the Webhook is applicable. A determinate Webhook result has exactly one terminal step.

Registered reason codes are:

| Reason code | Meaning |
| --- | --- |
| `ADMISSION_CONFIGURATION_EXCLUDED` | Webhook configuration resources exclude themselves |
| `AUTHORIZATION_CONTEXT_MISSING` | Required authorization fixture decision is absent |
| `CEL_AUTHORIZATION_ERROR` | Explicit authorization fixture error occurred during CEL |
| `CEL_COMPILE_ERROR` | A match condition did not compile |
| `CEL_COST_BUDGET_EXCEEDED` | The match-condition runtime cost budget was exhausted |
| `CEL_RUNTIME_ERROR` | A match condition failed during evaluation |
| `CAPABILITY_OUTSIDE_PROFILE` | Required semantics are unsupported by this profile |
| `EVALUATION_PROBLEM_DISCARDED` | A pending selector problem became irrelevant |
| `EVALUATION_PROBLEM_PENDING` | A selector problem is waiting for applicability |
| `EQUIVALENCE_CONTEXT_MISSING` | Required equivalence mapping is absent |
| `INTERNAL_ERROR` | An internal invariant or operation failed |
| `INVALID_INPUT` | Input violates the public contract |
| `KUBERNETES_EVALUATION_ERROR` | Kubernetes-compatible evaluation returned an error |
| `MATCH_CONDITIONS_TRUE` | All configured match conditions are true, including an empty list |
| `MATCH_CONDITION_FALSE` | A match condition is false |
| `MATCH_CONDITION_TRUE` | One match condition is true |
| `NAMESPACE_CONTEXT_MISSING` | Required Namespace fixture is absent |
| `NAMESPACE_SELECTOR_MATCH` | Namespace selector matched or was not applicable |
| `NAMESPACE_SELECTOR_NO_MATCH` | Namespace selector did not match |
| `OBJECT_SELECTOR_MATCH` | Object or oldObject matched the object selector |
| `OBJECT_SELECTOR_NO_MATCH` | Neither object nor oldObject matched |
| `RULE_MATCH` | An Exact or Equivalent candidate matched a rule |
| `RULE_NO_MATCH` | No applicable rule matched |
| `STAGE_NOT_RUN` | A stage was intentionally not evaluated |

Diagnostics repeat a registered `code` with `severity`, display `message`, `sourcePath`, and optional typed `missingContext` or `unsupportedCapability` detail. Scripts should branch on stable codes, not human-readable messages.

## CLI contract

```text
admitrace [--output text|json] explain --file <path|->
admitrace [--output text|json] test <path>...
admitrace [--output text|json] version
```

`--output` defaults to `text`. `explain` reads one file or stdin. `test` reads explicit files regardless of extension; directories recursively include only regular `.yaml`, `.yml`, and `.json` files without following symlink directories. One invocation is limited to 1,000 discovered documents.

JSON `test` output uses `admitrace.test/v1alpha1` and contains ordered `fixtures` plus a `summary` with `total`, `passed`, `mismatched`, `invalid`, `incomplete`, `internal`, and `exitCode`.

### Exit codes

When multiple fixtures fail, priority is internal error, invalid input, expectation mismatch, incomplete evaluation, then success.

| Code | Meaning | Example |
| --- | --- | --- |
| `0` | Determinate explanation or all asserted expectations match | `admitrace test docs/examples` |
| `1` | At least one `test` expectation mismatches | Change an expected `outcome: called` to `outcome: skipped` |
| `2` | CLI usage, Scenario schema, file, or resource-limit error | `admitrace explain` without `--file` |
| `3` | `explain` is incomplete, or `test` finds an unasserted incomplete result | Equivalent rule fallback without an equivalence fixture |
| `4` | Internal invariant, rendering, or output-write failure | An injected failing output writer, as covered by CLI process tests |

An exactly asserted `indeterminate` or `unsupported` expectation is a successful test and exits `0`.

These commands reproduce the user-controlled codes from the repository root after building `./build/admitrace`:

```sh
set +e

./build/admitrace test docs/examples                         # 0

sed 's/outcome: called/outcome: skipped/' \
  docs/examples/validating.yaml > /tmp/admitrace-mismatch.yaml
./build/admitrace test /tmp/admitrace-mismatch.yaml          # 1

./build/admitrace explain                                    # 2

sed -e 's/matchPolicy: Exact/matchPolicy: Equivalent/' \
  -e 's/apiVersions: \[v1\]/apiVersions: [v2]/' \
  docs/examples/validating.yaml > /tmp/admitrace-incomplete.yaml
./build/admitrace explain --file /tmp/admitrace-incomplete.yaml # 3

rm -f /tmp/admitrace-mismatch.yaml /tmp/admitrace-incomplete.yaml
```

The comments show the expected process status; remove `set +e` in normal CI so a nonzero status fails the step. Exit `4` protects internal and output-write failures and is exercised with an injected failing writer in the CLI test suite rather than through a supported user workflow.

## Mutating limitation

A Mutating result uses `mutating-initial-snapshot-eligibility`. It says whether each Webhook is eligible against the original supplied snapshot. AdmiTrace does not apply a returned JSON patch, feed a changed object to later Webhooks, or run reinvocation. It therefore cannot predict the final mutation chain.

## Explicit non-goals

The current product does not:

- send AdmissionReview HTTP/TLS requests;
- evaluate Webhook response allow/deny, status, warnings, audit annotations, or patches;
- negotiate transport, certificates, timeouts, or `AdmissionReviewVersions`;
- simulate mutating response ordering, patch application, or `reinvocationPolicy`;
- retrieve live Namespace, authorization, API discovery, CRD, or request snapshots;
- provide a command that captures a live AdmissionRequest snapshot from a cluster;
- emit JUnit XML;
- provide a project-specific adapter or upstream integration;
- promise a stable public Go API;
- approximate an unverified Kubernetes version.

The separate envtest suite uses a real local Kubernetes `1.36.2` API server only as a development oracle. It is not part of production runtime and does not change these non-goals.
