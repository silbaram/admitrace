#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"

results_file=$(mktemp)
trap 'rm -f "$results_file"' EXIT HUP INT TERM

if go test -json -count=1 \
	-run '^(TestResourceLimit|TestResourceLimits)' \
	./internal/contract \
	./internal/scenario \
	./internal/matchcondition \
	./internal/cli >"$results_file"; then
	cat "$results_file"
else
	status=$?
	cat "$results_file"
	exit "$status"
fi

for expected in \
	TestResourceLimitErrorCategoriesAndMessage \
	TestResourceLimitsRejectOversizedAndDeepDocuments \
	TestResourceLimitsRejectDocumentCount \
	TestResourceLimitsCELBudgetDiagnosticIsStable \
	TestResourceLimitsBoundReaderBeforeEndOfInput \
	TestResourceLimitsDiscoveryRejectsTooManyDocuments \
	TestResourceLimitsReadAndClosePreservesReadErrorPriority \
	TestResourceLimitsReadAndCloseReportsCloseError \
	TestResourceLimitsAcceptLongLineWithinByteLimit
do
	set +e
	grep -E '"Action":"run".*"Test":"'"$expected"'"' "$results_file" >/dev/null
	status=$?
	set -e
	case $status in
	0) ;;
	1)
		printf '%s\n' "resource-limits/fuzz: expected test did not run: $expected" >&2
		exit 1
		;;
	*)
		printf '%s\n' "resource-limits/fuzz: could not inspect test report for $expected" >&2
		exit "$status"
		;;
	esac
done

require_fuzz_target() {
	package=$1
	target=$2
	if listing=$(go test -list "^$target$" "$package"); then
		:
	else
		return $?
	fi
	set +e
	match=$(grep -x "$target" <<EOF
$listing
EOF
	)
	status=$?
	set -e
	case $status in
	0) ;;
	1)
		printf '%s\n' "resource-limits/fuzz: fuzz target is not selectable: $target" >&2
		return 1
		;;
	*)
		printf '%s\n' "resource-limits/fuzz: could not inspect fuzz target: $target" >&2
		return "$status"
		;;
	esac
	test "$match" = "$target"
}

fuzz_time=${ADMITRACE_FUZZTIME:-2s}
require_fuzz_target ./internal/scenario FuzzDecoderDeterministicClassification
go test ./internal/scenario \
	-run '^$' \
	-fuzz '^FuzzDecoderDeterministicClassification$' \
	-fuzztime "$fuzz_time"
require_fuzz_target ./internal/fixture FuzzFixtureContextDeterministicReplay
go test ./internal/fixture \
	-run '^$' \
	-fuzz '^FuzzFixtureContextDeterministicReplay$' \
	-fuzztime "$fuzz_time"

printf '%s\n' 'resource-limits/fuzz: passed'
