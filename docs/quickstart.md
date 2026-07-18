# Quickstart

English | [한국어](quickstart.ko.md)

AdmiTrace accepts both its replayable `Scenario` format and ordinary Kubernetes resources. Raw-resource mode evaluates `CREATE` routing and stays completely offline unless you explicitly select a kubeconfig context.

## Build

From the repository root:

```sh
mkdir -p ./build
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace version
```

## Explain resources offline

`-f/--file` is universal. A single `admitrace.io/v1alpha1` `Scenario` keeps the legacy result schema; every other file, stdin stream, or directory enters resource mode. `--resource` is an explicit resource-mode synonym.

Explain one resource with explicit WebhookConfiguration and Namespace files:

```sh
./build/admitrace explain \
  --resource docs/manifest-examples/resource.yaml \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml
```

Explain a two-document resource stream against both configuration documents as canonical JSON:

```sh
./build/admitrace --output json explain \
  -f docs/manifest-examples/resources.yaml \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml
```

The same resource stream can come from stdin, or from a directory whose YAML/JSON files are processed in lexical order:

```sh
./build/admitrace explain -f - \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml \
  < docs/manifest-examples/resources.yaml

./build/admitrace explain -f docs/manifest-examples/resource-directory \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml
```

The primary directory must contain resources only. Invalid documents are reported by logical filename and 1-based document index, and no partial output claims complete coverage.

Without `--context`, client construction and Kubernetes API traffic are both zero. Offline GVK resolution uses the generated Kubernetes `1.36.2` built-in catalog. An unknown GVK or CRD is `unsupported`; AdmiTrace never guesses its plural or scope.

## Opt in to limited hydration

Use an explicitly named context when a CRD needs discovery or when configuration/Namespace files are unavailable:

```sh
./build/admitrace explain \
  --resource docs/manifest-examples/resource.yaml \
  --context production \
  --kubeconfig /path/to/kubeconfig
```

Hydration first performs `GET /version` and continues only when the server is exactly `v1.36.2`—other patches, minors, and vendor suffixes are rejected. The remaining surface is GET-only: discovery, Validating/MutatingWebhookConfiguration LIST (HTTP GET), and a Namespace GET when a selected namespace selector needs it. AdmiTrace never issues SubjectAccessReview, dry-run, watch, or mutation requests.

Explicit files take precedence and suppress the corresponding cluster reads. This is useful when discovery is needed for a CRD but configuration or Namespace LIST/GET permission is unavailable:

```sh
./build/admitrace explain \
  --resource docs/manifest-examples/resource.yaml \
  --context production \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml
```

Kubeconfig credentials identify only the Kubernetes API connection. They never become the admission request identity. Supply that identity explicitly when `matchConditions` use `request.userInfo`:

```sh
./build/admitrace explain \
  --resource docs/manifest-examples/resource.yaml \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml \
  --user alice --group developers --user-extra tenant=blue
```

Missing configuration, Namespace, identity, equivalence, or authorization context remains fail-closed as `indeterminate`/`unsupported` with exit code `3` and file-fallback guidance.

## Export and replay snapshots

`--snapshot-out` writes one canonical Scenario per resource/configuration pair to a non-existent or empty directory:

```sh
snapshot_dir=$(mktemp -d)
./build/admitrace --output json explain \
  --resource docs/manifest-examples/resource.yaml \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml \
  --user alice \
  --snapshot-out "$snapshot_dir"

./build/admitrace explain -f "$snapshot_dir/0001-0001.yaml"
```

SnapshotPolicy is exact-copy-or-refuse. A core/v1 `Secret` is always refused. Explicit `UserInfo` and user-supplied general resources/CRDs are stored unchanged for exact replay; no field redaction or generic secret detection is promised for custom-resource fields. Kubeconfig bytes, credentials, API server URL, selected context, and automatic connection identity are not copied. Published directory/file modes are `0700`/`0600`.

## Preserve legacy Scenario behavior

The existing Scenario commands are unchanged:

```sh
./build/admitrace explain -f docs/examples/validating.yaml
./build/admitrace --output json explain -f docs/examples/mutating.yaml
./build/admitrace test docs/examples
```

`test` recursively discovers only Scenario fixtures and compares their expectations. It does not adapt raw resources.

## Read a result

Resource mode wraps each input resource in `admitrace.manifest-explanation/v1alpha1`, including source/document provenance, exact profile status, context completeness, diagnostics, and one ordered legacy evaluator result per configuration. Text and JSON are deterministic.

`called` means routing selected the Webhook. No AdmissionReview HTTP/TLS request is sent, and no response, allow/deny decision, patch, or reinvocation is observed. See the [Scenario and result reference](reference.md) for the full contract and exit codes.
