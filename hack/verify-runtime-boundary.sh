#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"

module_path=$(go list -m -f '{{.Path}}')
production_dirs=$(go list -buildvcs=false -deps -f \
	'{{if .Module}}{{if eq .Module.Path "'"$module_path"'"}}{{.Dir}}{{end}}{{end}}' \
	./cmd/admitrace)
production_files=$(
	for directory in $production_dirs; do
		find "$directory" -maxdepth 1 -type f -name '*.go' ! -name '*_test.go' -print
	done
)
if [ -z "$production_files" ]; then
	printf '%s\n' 'production source boundary resolved no files' >&2
	exit 1
fi
set +e
forbidden_source=$(grep -n -E \
	'"(net|net/http|syscall|golang.org/x/sys/unix|k8s.io/client-go/(kubernetes|dynamic|discovery|rest)(/[^" ]*)?|sigs.k8s.io/controller-runtime/pkg/envtest)"|\.(Dial|DialContext|Listen|ListenAndServe)\(' \
	$production_files)
source_status=$?
set -e
case $source_status in
0)
	printf '%s\n' 'production source contains a client, dial, listener, or envtest boundary violation:' >&2
	printf '%s\n' "$forbidden_source" >&2
	exit 1
;;
1) ;;
*)
	printf '%s\n' 'production source boundary inspection failed' >&2
	exit "$source_status"
;;
esac

production_dependencies=$(go list -buildvcs=false -deps ./cmd/admitrace)
set +e
forbidden_dependencies=$(grep -E \
	'(^|/)envtest($|/)|^sigs\.k8s\.io/controller-runtime($|/)' <<EOF
$production_dependencies
EOF
)
dependency_status=$?
set -e
case $dependency_status in
0)
	printf '%s\n' 'production dependency graph contains conformance-only code:' >&2
	printf '%s\n' "$forbidden_dependencies" >&2
	exit 1
;;
1) ;;
*)
	printf '%s\n' 'production dependency boundary inspection failed' >&2
	exit "$dependency_status"
;;
esac

printf '%s\n' 'verified offline production runtime boundary'
