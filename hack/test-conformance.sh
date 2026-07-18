#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

if [ -z "${KUBEBUILDER_ASSETS:-}" ]; then
	printf '%s\n' 'KUBEBUILDER_ASSETS must point to exact Kubernetes 1.36.2 envtest assets' >&2
	exit 2
fi

cd "$project_dir/conformance"

output_file=$(mktemp "${TMPDIR:-/tmp}/admitrace-conformance.XXXXXX")
cleanup() {
	rm -f "$output_file"
}
trap cleanup EXIT HUP INT TERM

if GOWORK=off GOPROXY=off go test -count=1 -mod=readonly -tags=conformance -v ./... >"$output_file" 2>&1; then
	:
else
	status=$?
	cat "$output_file"
	exit "$status"
fi
cat "$output_file"

for test_name in \
	TestSmokeObservations \
	TestSmokeObservations/called \
	TestSmokeObservations/skipped \
	TestSmokeObservations/rejected \
	TestEquivalentAfterExactMiss \
	TestEquivalentAfterExactMiss/exact \
	TestEquivalentAfterExactMiss/equivalent \
	TestManifestAdapterHydrationAndSnapshot \
	TestManifestAdapterHydrationAndSnapshot/catalog_and_CRD_discovery \
	TestManifestAdapterHydrationAndSnapshot/real_API_permission_denial \
	TestManifestAdapterHydrationAndSnapshot/Namespace_and_configuration_hydration \
	TestManifestAdapterHydrationAndSnapshot/offline_CRD_remains_unsupported \
	TestManifestAdapterHydrationAndSnapshot/snapshot_offline_replay
do
	if grep -F -q "=== RUN   $test_name" "$output_file"; then
		continue
	else
		status=$?
	fi
	if [ "$status" -ne 1 ]; then
		printf '%s\n' "failed to inspect conformance output for $test_name" >&2
		exit "$status"
	fi
	printf '%s\n' "conformance false pass: $test_name did not execute" >&2
	exit 1
done
