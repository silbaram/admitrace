# Manifest Adapter Release Readiness

English | [한국어](release-readiness.ko.md)

The release channel combines the legacy offline Scenario evaluator with the universal manifest adapter and explicit, GET-only hydration. Run the release gate from a clean checkout with the pinned Go `1.26.5` toolchain, cached module dependencies, and the exact Kubernetes `1.36.2` envtest binaries:

```sh
export KUBEBUILDER_ASSETS=/path/to/k8s/1.36.2-platform-arch
GO_BIN=/path/to/go1.26.5/bin/go ./hack/verify-release-readiness.sh
```

Set `ADMITRACE_FUZZTIME` to change the default two-second duration of each fuzz target. Set `PARITY_REPORT` to retain the deterministic parity JSON at a chosen path; relative paths resolve from the repository root. Otherwise the gate uses a temporary report.

The command exits zero only after every item below passes.

## Automated checklist

- [x] Root unit, golden, CLI process, resource-limit, deterministic-output, offline manifest E2E, hydration-security, SnapshotPolicy, and legacy-path tests pass and their named sentinels execute.
- [x] Both required fuzz targets are selectable and execute.
- [x] Go language `1.26.0`, toolchain `go1.26.5`, Cobra `v1.10.2`, Kubernetes modules `v0.36.2`, envtest `v0.24.1`, and control-plane `1.36.2` are pinned.
- [x] The production dependency graph excludes the `k8s.io/kubernetes` root module, envtest, and controller-runtime; client/network code is isolated to `internal/hydration`, whose transport and exact request surface are independently audited as GET-only.
- [x] The generated Kubernetes `1.36.2` built-in resource catalog regenerates without drift and validates against exact envtest discovery.
- [x] A standalone `admitrace` binary runs `version`, legacy Validating/Mutating `explain`, directory `test`, offline single/multi/directory manifest examples, and snapshot replay.
- [x] Repeated standalone JSON legacy and multi-manifest `explain` plus `test` output is byte-identical.
- [x] All 29 determinate parity cases have independent Kubernetes `1.36.2` authority: 21 direct API server observations plus 8 official matcher differentials; 4 incomplete contracts remain separate.
- [x] The parity report records exact oracle-kind coverage and zero semantic or differential mismatches; a reviewed golden trace cannot pass a case by itself.
- [x] Required envtest conformance proves the exact `1.36.2` catalog, CRD discovery/scope, status-subresource routing, real RBAC denial, bounded configuration/Namespace hydration, and canonical replay.
- [x] The complete conformance suite and both public beta project cases pass against the pinned local API server.
- [x] Executable help and documentation cover universal `-f`, explicit `--resource`, source/document provenance, `CREATE` only, exact-version GET-only hydration, explicit identity, offline CRD refusal, SnapshotPolicy, and routing-only `called`.
- [x] Every quickstart manifest command is exercised against the committed examples and expected output schema by this gate.

The envtest API server, loopback TLS recorders, and official Kubernetes matcher differentials are development oracles used only by the release gate. The standalone product performs cluster reads only after an explicit context opt-in and only through the documented GET surface; it never performs Webhook transport. A passing gate does not expand the product into response evaluation, patch application, reinvocation, live AdmissionRequest capture, or project-specific adapters.
