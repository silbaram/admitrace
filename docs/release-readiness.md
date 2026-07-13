# v0.1 Release Readiness

English | [한국어](release-readiness.ko.md)

The v0.1 release channel is a public beta of the approved offline snapshot-routing scope. Run the release gate from a clean checkout with the pinned Go `1.26.5` toolchain, cached module dependencies, and the exact Kubernetes `1.36.2` envtest binaries:

```sh
export KUBEBUILDER_ASSETS=/path/to/k8s/1.36.2-platform-arch
GO_BIN=/path/to/go1.26.5/bin/go ./hack/verify-release-readiness.sh
```

Set `ADMITRACE_FUZZTIME` to change the default two-second duration of each fuzz target. Set `PARITY_REPORT` to retain the deterministic parity JSON at a chosen path; relative paths resolve from the repository root. Otherwise the gate uses a temporary report.

The command exits zero only after every item below passes.

## Automated checklist

- [x] Root unit, golden, CLI process, resource-limit, and deterministic-output tests pass.
- [x] Both required fuzz targets are selectable and execute.
- [x] Go language `1.26.0`, toolchain `go1.26.5`, Cobra `v1.10.2`, Kubernetes modules `v0.36.2`, envtest `v0.24.1`, and control-plane `1.36.2` are pinned.
- [x] The production dependency graph excludes the `k8s.io/kubernetes` root module, envtest, controller-runtime, live clients, listeners, and network dialers.
- [x] A standalone `admitrace` binary runs `version`, Validating and Mutating `explain`, and directory `test` smoke cases without runtime network access.
- [x] Repeated standalone JSON `explain` and `test` output is byte-identical.
- [x] The Kubernetes `1.36.2` parity matrix contains at least 20 cases and its report records zero semantic mismatches.
- [x] The complete conformance suite and both public beta project cases pass against the pinned local API server.
- [x] The support policy, Mutating initial-snapshot limitation, and explicit non-goals remain documented.

The envtest API server and loopback TLS recorders are development oracles used only by the release gate. The standalone product still performs no live cluster lookup or Webhook transport. A passing gate does not expand v0.1 into response evaluation, patch application, reinvocation, live snapshot capture, or project-specific adapters.
