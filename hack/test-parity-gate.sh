#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_bin=${GO_BIN:-go}
if ! command -v "$go_bin" >/dev/null 2>&1; then
	printf '%s\n' "Go executable not found: $go_bin" >&2
	exit 2
fi
go_bin=$(command -v "$go_bin")
report=${PARITY_REPORT:-${TMPDIR:-/tmp}/admitrace-parity-kubernetes-1.36.2.json}
cd "$project_dir/conformance"
env GOWORK=off GOPROXY=off "$go_bin" run -mod=readonly ./cmd/paritygate -go "$go_bin" -report "$report"
printf '%s\n' "parity report: $report"
