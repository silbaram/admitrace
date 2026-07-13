# Kubernetes conformance oracle

This directory is a separate Go module. Its `controller-runtime/envtest` and
Kubernetes client dependencies are intentionally excluded from the production
`admitrace` module and binary graph.

The harness is pinned to:

- `sigs.k8s.io/controller-runtime` `v0.24.1`
- Kubernetes Go modules `v0.36.2`
- Kubernetes control-plane assets `1.36.2`

Release parity uses two independent test-only oracle tiers: 21 direct control-plane observations and 8 differentials through official Kubernetes `v0.36.2` matcher or predicate packages. The production module does not gain conformance-only clients or envtest dependencies.

The suite never downloads binaries. Set `KUBEBUILDER_ASSETS` to a directory
containing executable `kube-apiserver` and `etcd` binaries. The harness executes
`kube-apiserver --version` before startup and rejects every version other than
exactly `v1.36.2` as an oracle setup failure.

After dependencies and the exact assets have been provisioned explicitly, run:

```sh
cd conformance
KUBEBUILDER_ASSETS=/absolute/path/to/kubernetes-1.36.2 \
  go test -tags=conformance ./...
```

A missing dependency or asset is not a skipped semantic case: the conformance
command fails and reports the `assets`, `control-plane`, or `tls` setup stage.
Ordinary root-module tests do not enter this nested module.
