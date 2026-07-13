#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go_bin="${GO:-go}"
binary="$(mktemp "${TMPDIR:-/tmp}/admitrace-beta.XXXXXX")"
report_output="$(mktemp "${TMPDIR:-/tmp}/admitrace-beta-report.XXXXXX")"
oracle_output="$(mktemp "${TMPDIR:-/tmp}/admitrace-beta-oracle.XXXXXX")"
cleanup() {
  rm -f "$binary" "$report_output" "$oracle_output"
}
trap cleanup EXIT HUP INT TERM

cd "$repo_root"

if [[ -z "${KUBEBUILDER_ASSETS:-}" ]]; then
  echo "KUBEBUILDER_ASSETS must point to the pinned Kubernetes 1.36.2 envtest binaries" >&2
  exit 2
fi

"$go_bin" build -o "$binary" ./cmd/admitrace
"$binary" test validation/beta/scenarios
"$binary" --output json test validation/beta/scenarios
if "$go_bin" test -count=1 -v ./validation/beta >"$report_output" 2>&1; then
  :
else
  status=$?
  cat "$report_output"
  exit "$status"
fi
cat "$report_output"

for test_name in \
  TestReportMatchesFreshScenarioEvaluation \
  TestReportMatchesFreshScenarioEvaluation/OPA_Gatekeeper \
  TestReportMatchesFreshScenarioEvaluation/Istio
do
  if grep -F -q "=== RUN   $test_name" "$report_output"; then
    continue
  fi
  echo "beta validation false pass: $test_name did not execute" >&2
  exit 1
done

if "$go_bin" -C conformance test -count=1 -tags=conformance -v ./oracle -run '^TestBetaProjectParity$' >"$oracle_output" 2>&1; then
  :
else
  status=$?
  cat "$oracle_output"
  exit "$status"
fi
cat "$oracle_output"

for test_name in \
  TestBetaProjectParity \
  TestBetaProjectParity/gatekeeper-v3.22.2-validating \
  TestBetaProjectParity/istio-1.30.0-mutating
do
  if grep -F -q "=== RUN   $test_name" "$oracle_output"; then
    continue
  fi
  echo "beta validation false pass: $test_name did not execute" >&2
  exit 1
done
