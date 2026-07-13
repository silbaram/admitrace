#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"

output_file=$(mktemp "${TMPDIR:-/tmp}/admitrace-boundary.XXXXXX")
cleanup() {
	rm -f "$output_file"
}
trap cleanup EXIT HUP INT TERM

require_exact_module() {
	module=$1
	version=$2
	if declaration=$(awk -v module="$module" '$1 == module { print $2 }' conformance/go.mod); then
		:
	else
		status=$?
		printf '%s\n' "failed to inspect conformance/go.mod for $module" >&2
		exit "$status"
	fi
	if [ "$declaration" != "$version" ]; then
		printf '%s\n' "$module declares ${declaration:-nothing}; want $version" >&2
		exit 1
	fi
}

assert_no_match() {
	description=$1
	pattern=$2
	shift 2
	if grep -q -E "$pattern" "$@"; then
		printf '%s\n' "$description" >&2
		exit 1
	else
		status=$?
	fi
	if [ "$status" -ne 1 ]; then
		printf '%s\n' "failed to inspect dependency boundary: $description" >&2
		exit "$status"
	fi
}

require_line() {
	line=$1
	file=$2
	if grep -q -x -F "$line" "$file"; then
		return
	else
		status=$?
	fi
	if [ "$status" -ne 1 ]; then
		printf '%s\n' "failed to inspect $file" >&2
		exit "$status"
	fi
	printf '%s\n' "$file is missing required line: $line" >&2
	exit 1
}

require_exact_module sigs.k8s.io/controller-runtime v0.24.1
require_exact_module k8s.io/api v0.36.2
require_exact_module k8s.io/apiextensions-apiserver v0.36.2
require_exact_module k8s.io/apimachinery v0.36.2
require_exact_module k8s.io/client-go v0.36.2

assert_no_match \
	'conformance-only modules leaked into the production module' \
	'sigs\.k8s\.io/controller-runtime|k8s\.io/apiextensions-apiserver' \
	go.mod go.sum

if go list -buildvcs=false -deps ./cmd/admitrace >"$output_file"; then
	:
else
	status=$?
	printf '%s\n' 'failed to resolve the production binary dependency graph' >&2
	exit "$status"
fi
assert_no_match \
	'envtest leaked into the production binary dependency graph' \
	'^sigs\.k8s\.io/controller-runtime/pkg/envtest($|/)' \
	"$output_file"
assert_no_match \
	'forbidden k8s.io/kubernetes root module reference found' \
	'(^|[[:space:]])k8s\.io/kubernetes([[:space:]]|$)' \
	go.mod go.sum conformance/go.mod conformance/go.sum

require_line 'ENVTEST_VERSION=0.24.1' conformance/versions.env
require_line 'KUBERNETES_VERSION=1.36.2' conformance/versions.env
require_line 'KUBERNETES_MODULE_VERSION=0.36.2' conformance/versions.env

printf '%s\n' 'verified test-only envtest v0.24.1 and Kubernetes v1.36.2 boundary'
