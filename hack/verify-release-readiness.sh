#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"

fail() {
	printf '%s\n' "release readiness: $*" >&2
	exit 1
}

require_text() {
	text=$1
	file=$2
	if grep -F -q "$text" "$file"; then
		return
	else
		status=$?
	fi
	if [ "$status" -ne 1 ]; then
		printf '%s\n' "release readiness: failed to inspect $file" >&2
		exit "$status"
	fi
	fail "$file is missing required release evidence: $text"
}

require_line() {
	line=$1
	file=$2
	if grep -F -x -q "$line" "$file"; then
		return
	else
		status=$?
	fi
	if [ "$status" -ne 1 ]; then
		printf '%s\n' "release readiness: failed to inspect $file" >&2
		exit "$status"
	fi
	fail "$file is missing required exact line: $line"
}

requested_go=${GO_BIN:-${GO:-go}}
if ! command -v "$requested_go" >/dev/null 2>&1; then
	printf '%s\n' "release readiness: Go executable not found: $requested_go" >&2
	exit 2
fi
go_bin=$(command -v "$requested_go")
PATH=$(dirname "$go_bin"):$PATH
GO_BIN=$go_bin
GO=$go_bin
export PATH GO_BIN GO

case $("$go_bin" version) in
"go version go1.26.5 "*) ;;
*) fail "Go executable must report go1.26.5" ;;
esac

if [ -z "${KUBEBUILDER_ASSETS:-}" ]; then
	printf '%s\n' 'release readiness: KUBEBUILDER_ASSETS must point to Kubernetes 1.36.2 envtest binaries' >&2
	exit 2
fi
for asset in kube-apiserver etcd kubectl; do
	if [ ! -x "$KUBEBUILDER_ASSETS/$asset" ]; then
		printf '%s\n' "release readiness: missing executable envtest asset: $KUBEBUILDER_ASSETS/$asset" >&2
		exit 2
	fi
done
if [ "$("$KUBEBUILDER_ASSETS/kube-apiserver" --version)" != 'Kubernetes v1.36.2' ]; then
	fail "kube-apiserver must report Kubernetes v1.36.2"
fi

temporary_dir=$(mktemp -d "${TMPDIR:-/tmp}/admitrace-release.XXXXXX")
cleanup() {
	rm -rf "$temporary_dir"
}
trap cleanup EXIT HUP INT TERM

binary=$temporary_dir/admitrace
version_text=$temporary_dir/version.txt
version_json=$temporary_dir/version.json
explain_first=$temporary_dir/explain-first.json
explain_second=$temporary_dir/explain-second.json
test_first=$temporary_dir/test-first.json
test_second=$temporary_dir/test-second.json
root_tests=$temporary_dir/root-tests.json
format_output=$temporary_dir/gofmt.txt
parity_report=${PARITY_REPORT:-$temporary_dir/parity-report.json}
case $parity_report in
/*) ;;
*) parity_report=$project_dir/$parity_report ;;
esac

require_line 'go 1.26.0' go.mod
require_line 'toolchain go1.26.5' go.mod
require_line 'go 1.26.0' conformance/go.mod
require_line 'toolchain go1.26.5' conformance/go.mod
if ! awk '
	$1 == "github.com/spf13/cobra" && $2 == "v1.10.2" && index($0, "// indirect") == 0 { count++ }
	END { exit count == 1 ? 0 : 1 }
' go.mod; then
	fail 'go.mod must declare github.com/spf13/cobra v1.10.2 exactly once as a direct dependency'
fi
resolved_cobra=$("$go_bin" list -m -mod=readonly -f '{{.Version}}' github.com/spf13/cobra)
if [ "$resolved_cobra" != 'v1.10.2' ]; then
	fail "github.com/spf13/cobra resolves to $resolved_cobra; want v1.10.2"
fi

"$go_bin" mod verify
gofmt_bin=$(dirname "$go_bin")/gofmt
if [ ! -x "$gofmt_bin" ]; then
	printf '%s\n' "release readiness: gofmt executable not found: $gofmt_bin" >&2
	exit 2
fi
if "$gofmt_bin" -l . >"$format_output"; then
	:
else
	status=$?
	cat "$format_output"
	exit "$status"
fi
if [ -s "$format_output" ]; then
	cat "$format_output" >&2
	fail 'gofmt reported unformatted Go files'
fi

if "$go_bin" test -json -count=1 ./... >"$root_tests"; then
	:
else
	status=$?
	cat "$root_tests"
	exit "$status"
fi
cat "$root_tests"
for test_name in \
	TestContractGolden \
	TestGoldenResults \
	TestExecuteExplainFileAndStdinAreEquivalent \
	TestExecuteTestOutputFormatsAreDeterministic \
	TestVersionProcessUsesBuildMetadata
do
	if grep -E -q '"Action":"run".*"Test":"'"$test_name"'"' "$root_tests"; then
		continue
	else
		status=$?
	fi
	if [ "$status" -ne 1 ]; then
		printf '%s\n' "release readiness: failed to inspect root test output for $test_name" >&2
		exit "$status"
	fi
	fail "root test did not execute: $test_name"
done
"$go_bin" vet ./...
./hack/verify-dependencies.sh
./hack/verify-runtime-boundary.sh
./hack/verify-conformance-boundary.sh
./hack/test-redaction-offline-determinism.sh
./hack/test-resource-limits-fuzz.sh

"$go_bin" build -buildvcs=false -trimpath \
	-ldflags '-X main.version=v0.1.0-beta -X main.commit=release-readiness -X main.buildDate=1970-01-01T00:00:00Z' \
	-o "$binary" ./cmd/admitrace

"$binary" version >"$version_text"
"$binary" --output json version >"$version_json"
require_text 'AdmiTrace v0.1.0-beta' "$version_text"
require_text 'Go toolchain: go1.26.5' "$version_text"
require_text 'dependency: github.com/spf13/cobra v1.10.2' "$version_text"
require_text '"id": "kubernetes-1.36.2-defaults"' "$version_json"

"$binary" explain -f docs/examples/validating.yaml >/dev/null
"$binary" explain -f docs/examples/mutating.yaml >/dev/null
"$binary" --output json explain -f docs/examples/validating.yaml >"$explain_first"
"$binary" --output json explain -f docs/examples/validating.yaml >"$explain_second"
cmp "$explain_first" "$explain_second"

"$binary" test docs/examples >/dev/null
"$binary" --output json test docs/examples >"$test_first"
"$binary" --output json test docs/examples >"$test_second"
cmp "$test_first" "$test_second"

PARITY_REPORT="$parity_report" ./hack/test-parity-gate.sh
./hack/test-conformance.sh
./hack/test-beta-validation.sh

"$go_bin" run -mod=readonly ./hack/releasecheck \
	-parity "$parity_report" \
	-beta validation/beta/report.json

require_text '## Supported scope' README.md
require_text '## Out of scope' README.md
require_text '## Mutating limitation' docs/reference.md
require_text '## Explicit non-goals' docs/reference.md
require_text '## 지원 범위' README.ko.md
require_text '## 비범위' README.ko.md
require_text '## Mutating 제한과 명시적 비범위' docs/reference.ko.md
require_text 'gatekeeper-v3.22.2-validating.yaml' validation/beta/README.md
require_text 'istio-1.30.0-mutating.yaml' validation/beta/README.md
require_text 'docs/release-readiness.md' README.md
require_text 'docs/release-readiness.ko.md' README.ko.md
require_text 'verify-release-readiness.sh' docs/release-readiness.md
require_text 'verify-release-readiness.sh' docs/release-readiness.ko.md

printf '%s\n' 'release readiness: passed'
if [ -n "${PARITY_REPORT:-}" ]; then
	printf '%s\n' "release readiness: parity report: $parity_report"
fi
