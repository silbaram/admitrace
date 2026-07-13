#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_bin=${GO_BIN:-go}
if ! command -v "$go_bin" >/dev/null 2>&1; then
	printf '%s\n' "Go executable not found: $go_bin" >&2
	exit 2
fi
if [ -z "${KUBEBUILDER_ASSETS:-}" ]; then
	printf '%s\n' 'KUBEBUILDER_ASSETS must point to exact Kubernetes 1.36.2 envtest assets' >&2
	exit 2
fi

cd "$project_dir/conformance"
output_file=$(mktemp "${TMPDIR:-/tmp}/admitrace-cel-authorizer.XXXXXX")
cleanup() {
	rm -f "$output_file"
}
trap cleanup EXIT HUP INT TERM

run_and_require() {
	test_name=$1
	shift
	if "$@" >"$output_file" 2>&1; then
		:
	else
		status=$?
		cat "$output_file"
		exit "$status"
	fi
	cat "$output_file"
	if grep -F -q "=== RUN   $test_name" "$output_file"; then
		return
	else
		status=$?
	fi
	if [ "$status" -ne 1 ]; then
		printf '%s\n' "failed to inspect test output for $test_name" >&2
		exit "$status"
	fi
	printf '%s\n' "parity false pass: $test_name did not execute" >&2
	exit 1
}

run_and_require TestCELAuthorizerParity env GOWORK=off GOPROXY=off "$go_bin" test -count=1 -mod=readonly -v ./parity/celauthorizer
run_and_require TestKubeAPIServerObservations env GOWORK=off GOPROXY=off "$go_bin" test -count=1 -mod=readonly -tags=conformance -run '^TestKubeAPIServerObservations$' -v ./parity/celauthorizer
