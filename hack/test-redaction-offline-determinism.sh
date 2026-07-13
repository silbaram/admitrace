#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"

results_file=$(mktemp)
trap 'rm -f "$results_file"' EXIT HUP INT TERM

if go test -json -count=1 \
	-run '^TestRedactionOfflineDeterminism' \
	./internal/evaluation \
	./internal/cli >"$results_file"; then
	cat "$results_file"
else
	status=$?
	cat "$results_file"
	exit "$status"
fi

for expected in \
	TestRedactionOfflineDeterminismSensitiveValuesStayOutOfResults \
	TestRedactionOfflineDeterminismMissingAuthorizationReferenceIsRedacted \
	TestRedactionOfflineDeterminismRuntimeSucceedsWithoutNetwork
do
	set +e
	grep -E '"Action":"run".*"Test":"'"$expected"'"' "$results_file" >/dev/null
	status=$?
	set -e
	case $status in
	0) ;;
	1)
		printf '%s\n' "redaction/offline/determinism: expected test did not run: $expected" >&2
		exit 1
		;;
	*)
		printf '%s\n' "redaction/offline/determinism: could not inspect test report for $expected" >&2
		exit "$status"
		;;
	esac
done

"$project_dir/hack/verify-runtime-boundary.sh"

printf '%s\n' 'redaction/offline/determinism: passed'
