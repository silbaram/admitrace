# Advanced parity fixtures

Task 021 keeps advanced fixtures in two independent groups:

- `equivalentselector`: Exact-first and Equivalent fallback, explicit/missing/unsupported mappings, and deferred selector errors.
- `celauthorizer`: match-condition reduction, CEL compile/runtime/cost failures, and recorded authorizer decisions.

Every `parity.Case` declares exactly one closed `oracleType`:

- `kube-apiserver-observation` records a Kubernetes 1.36.2 call, skip, or rejection using the envtest control plane.
- `golden-trace` compares the complete stable trace subset with reviewed Kubernetes-derived expectations.
- `incomplete-contract` requires a nil outcome plus an expected diagnostic and terminal trace because context is missing or semantics are unsupported.

Coverage tags are sorted and reported with the deterministic Scenario count. Run each group separately from the repository root:

```sh
KUBEBUILDER_ASSETS=/path/to/kubernetes/1.36.2/assets ./hack/test-parity-equivalent-selector.sh
KUBEBUILDER_ASSETS=/path/to/kubernetes/1.36.2/assets ./hack/test-parity-cel-authorizer.sh
```

Both scripts run the offline product comparison first and then the pinned API server observation. They fail if either expected test entry point does not execute.

## Unified release gate

Task 022 combines the core and advanced suites in a checked 33-case matrix. The matrix fixes every case to the `kubernetes-1.36.2-defaults` profile and rejects duplicate Scenario ids, missing required branch tags, unregistered oracle categories, and profile drift.

Run the release gate from the repository root:

```sh
KUBEBUILDER_ASSETS=/path/to/kubernetes/1.36.2/assets ./hack/test-parity-gate.sh
```

Set `PARITY_REPORT` to choose the report path. The default is `${TMPDIR}/admitrace-parity-kubernetes-1.36.2.json`. The report has no timestamp, duration, host path, or other nondeterministic field. It reports setup failures, determinate kube-apiserver semantic mismatches, golden contract failures, incomplete contract failures, and other contract failures separately. The gate passes only when all 18 determinate API server observations match and all offline contracts pass.
