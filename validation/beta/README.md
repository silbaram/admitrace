# Public beta Webhook validation

English | [한국어](README.ko.md)

This evidence set evaluates two independently maintained, public Webhook configurations before the AdmiTrace beta. It tests routing eligibility only. It does not evaluate either project's policy decision, Webhook response, transport, patch, or reinvocation behavior.

## Source and license gate

Both candidates passed the source gate before their Scenario was authored. They are public project-owned files at immutable release commits, and both repositories publish the source under Apache-2.0. The repository links and hashes below are the permission and reproducibility basis for this engineering validation; they are not legal advice.

| Project | Pinned source | SHA-256 | License evidence |
| --- | --- | --- | --- |
| OPA Gatekeeper `v3.22.2` (`eda110bdaf2510288dccd73a1be4dd0c6442a4aa`) | [`deploy/gatekeeper.yaml`](https://raw.githubusercontent.com/open-policy-agent/gatekeeper/eda110bdaf2510288dccd73a1be4dd0c6442a4aa/deploy/gatekeeper.yaml) | `72683f57fdfa4c34d4a892e5e6f457a5a7e533eba0293d781d53d08dd6614a5a` | [Apache-2.0 LICENSE](https://raw.githubusercontent.com/open-policy-agent/gatekeeper/eda110bdaf2510288dccd73a1be4dd0c6442a4aa/LICENSE), SHA-256 `c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4` |
| Istio `1.30.0` (`badd809ed7d57954d4c16e12e75e15a7722a7b96`) | [`mutatingwebhook.yaml`](https://raw.githubusercontent.com/istio/istio/badd809ed7d57954d4c16e12e75e15a7722a7b96/manifests/charts/istio-control/istio-discovery/templates/mutatingwebhook.yaml) | `0d0c1fdf2f607ce2eed45e68a37ed31e7301fc99f4853c833c91e1a2ab559223` | [Apache-2.0 LICENSE](https://raw.githubusercontent.com/istio/istio/badd809ed7d57954d4c16e12e75e15a7722a7b96/LICENSE), SHA-256 `9fa6e54dafda853bb3cdc01486b677a55102f0d488282a85ba6e426d9125f8c5` |

The Istio template was rendered from its same-release [`values.yaml`](https://raw.githubusercontent.com/istio/istio/badd809ed7d57954d4c16e12e75e15a7722a7b96/manifests/charts/istio-control/istio-discovery/values.yaml), SHA-256 `25a8185104caeeca0b8224fc2c78a7eea2bed9673f29fe99cf2a1c5bc72046e6`, with the default empty revision, `enableNamespacesByDefault: false`, `reinvocationPolicy: Never`, and `/inject` path.

Gatekeeper's pinned [NOTICE](https://raw.githubusercontent.com/open-policy-agent/gatekeeper/eda110bdaf2510288dccd73a1be4dd0c6442a4aa/NOTICE), SHA-256 `7d4302ff15270639b7f8d25e9dbfb988b6d4075a36d0e47fdb0d4a91661b18d7`, identifies “Gatekeeper, Copyright 2018-2020 The Gatekeeper Authors”; this attribution is retained here.

## Transformation and anonymization

- The public Webhook names, order, rules, selectors, failure policy, side effects, admission review versions, and relevant defaults are preserved.
- Omitted routing fields remain omitted in the YAML and resolve through the same Kubernetes `1.36.2` defaults during decoding and API registration.
- Cluster-generated metadata, CA bundles, release-manager labels, and unrelated workload objects are not copied.
- Request UIDs, users, namespaces, names, labels, images, and resource bodies are synthetic. There are no private project or customer identifiers to retain.
- The conformance oracle changes only each `clientConfig` to a dedicated loopback TLS recorder. The production evaluator still performs no network access.
- The Gatekeeper fixture uses the two validating Webhooks from `gatekeeper-validating-webhook-configuration`. The Istio fixture renders the four default-revision sidecar injector branches.

The reproducible Scenarios are:

- [`gatekeeper-v3.22.2-validating.yaml`](scenarios/gatekeeper-v3.22.2-validating.yaml)
- [`istio-1.30.0-mutating.yaml`](scenarios/istio-1.30.0-mutating.yaml)

## Reproduce

From the repository root, point `KUBEBUILDER_ASSETS` at the pinned Kubernetes `1.36.2` envtest binaries and run:

```sh
export KUBEBUILDER_ASSETS=/path/to/k8s/1.36.2-platform-arch
./hack/test-beta-validation.sh
```

The script builds the current CLI, runs both `admitrace test` text and JSON flows, checks the machine-readable report against fresh evaluator results, and then runs the Kubernetes API server oracle.

For the offline portion only:

```sh
go build -o /tmp/admitrace-beta ./cmd/admitrace
/tmp/admitrace-beta test validation/beta/scenarios
/tmp/admitrace-beta --output json test validation/beta/scenarios
go test -count=1 ./validation/beta
```

## Results

The canonical record is [`report.json`](report.json).

| Project | Configuration | Webhook observations | Incomplete |
| --- | --- | --- | --- |
| Gatekeeper | Validating, two Webhooks | `validation.gatekeeper.sh`: `called` / `MATCH_CONDITIONS_TRUE`; ignore-label Webhook: `skipped` / `RULE_NO_MATCH` | `0/2` |
| Istio | Mutating, four rendered branches | namespace-label branch: `called` / `MATCH_CONDITIONS_TRUE`; three other branches: `skipped` / `NAMESPACE_SELECTOR_NO_MATCH` | `0/4` |

Across six Webhook results, all six are determinate, two are called, four are skipped, and the incomplete rate is `0%`. Kubernetes `1.36.2` envtest observed the same per-Webhook call pattern, so the semantic mismatch count is zero.

### Trace understandability

- Gatekeeper's skipped second Webhook terminates at `exactRules`, and its `sourcePath` points to that Webhook's rules rather than leaving the skip ambiguous.
- Istio's three excluded branches intentionally share `NAMESPACE_SELECTOR_NO_MATCH`. Their stable Webhook names, indices, paths, and namespace input summaries make the rendered branch identifiable.
- The called branches continue through selectors and exact rules to terminal `MATCH_CONDITIONS_TRUE`, which makes the selection sequence visible without implying a successful remote call.

### Incomplete and unsupported review

No Webhook result is `indeterminate` or `unsupported`; no required Namespace fixture is missing. Informational `CAPABILITY_OUTSIDE_PROFILE` diagnostics still mark the deliberately unsupported boundaries:

- AdmissionReview negotiation and HTTP/TLS transport are not part of the offline product result.
- A Webhook response and the underlying Gatekeeper policy or Istio injection decision are not evaluated.
- The Istio result is initial-snapshot eligibility only; patches and `reinvocationPolicy` are not simulated.

These diagnostics do not convert a routing result into an unsupported determination because the requested snapshot routing itself is fully evaluable.

## Scope disposition

The validation found no scope change required for beta and added no product behavior. Response evaluation, patch application, reinvocation, live-cluster capture, and project adapters remain possible future-iteration candidates only if real user demand is established.
