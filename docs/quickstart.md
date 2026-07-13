# Quickstart

English | [한국어](quickstart.ko.md)

AdmiTrace evaluates one supplied request snapshot against one validating or mutating Webhook configuration. It runs offline and does not need a cluster.

## Build

From the repository root:

```sh
mkdir -p ./build
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace version
```

The repository includes two executable Scenario examples:

- [`examples/validating.yaml`](examples/validating.yaml): a validating Pod `CREATE` selected for invocation.
- [`examples/mutating.yaml`](examples/mutating.yaml): a mutating ConfigMap `UPDATE` selected from the initial request snapshot.

## Explain one Scenario

Text is the default output and is intended for a person reading the ordered routing trace:

```sh
./build/admitrace explain --file docs/examples/validating.yaml
```

Canonical JSON is intended for tools and preserves array order and absent-versus-empty distinctions:

```sh
./build/admitrace --output json explain --file docs/examples/validating.yaml
```

Use `--file -` to read exactly one Scenario from standard input:

```sh
./build/admitrace explain --file - < docs/examples/validating.yaml
```

Normal results go to stdout. Usage, invalid-input, and internal diagnostics go to stderr. `explain` exits `0` for a fully determinate result and `3` if any Webhook is `indeterminate` or `unsupported`.

## Check expectations in CI

`test` accepts explicit files and directories. Directories are searched recursively for regular `.yaml`, `.yml`, and `.json` files; discovered clean paths are deduplicated and evaluated in lexical order.

```sh
./build/admitrace test docs/examples
./build/admitrace --output json test docs/examples
```

Both included fixtures declare matching expectations, so these commands exit `0`. A determination, asserted outcome, or asserted terminal reason mismatch exits `1`. An exactly expected `indeterminate` or `unsupported` result exits `0`; an unasserted incomplete result exits `3`.

A minimal CI flow is:

```sh
set -eu
mkdir -p ./build
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace --output json test docs/examples
```

Only text and JSON reports are supported. AdmiTrace does not emit JUnit XML.

## Read a result

Each Webhook result contains:

- `determination`: whether AdmiTrace could complete the supported evaluation.
- optional `outcome`: `called`, `skipped`, or `rejected-before-call` for a determinate result.
- `trace`: ordered steps with stable `reasonCode`, `pending`, `discarded`, and `terminal` state.
- `diagnostics`: structured missing-context, unsupported-capability, or evaluation information.

`called` means selected for invocation. No HTTP/TLS request is sent and no Webhook response is evaluated. Mutating results are initial-snapshot eligibility only; patches and reinvocation are not simulated.

See the [Scenario and result reference](reference.md) for the complete contract, exit codes, reason codes, support policy, and non-goals.
