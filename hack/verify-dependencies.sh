#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"

expected_version=v0.36.2
kubernetes_modules='k8s.io/api k8s.io/apimachinery k8s.io/apiserver k8s.io/client-go'

root_module_references=$(grep -n -E '(^|[[:space:]])k8s\.io/kubernetes([[:space:]]|$)' go.mod go.sum || true)
if [ -n "$root_module_references" ]; then
	printf '%s\n' 'forbidden k8s.io/kubernetes module reference found:' >&2
	printf '%s\n' "$root_module_references" >&2
	exit 1
fi

dependency_packages=$(go list -buildvcs=false -deps -test -f '{{.ImportPath}}' ./...)
root_dependency_packages=$(
	printf '%s\n' "$dependency_packages" |
		grep -E '^k8s\.io/kubernetes(/|$)' || true
)
if [ -n "$root_dependency_packages" ]; then
	printf '%s\n' 'forbidden k8s.io/kubernetes dependency package found:' >&2
	printf '%s\n' "$root_dependency_packages" >&2
	exit 1
fi

for module in $kubernetes_modules; do
	declaration=$(
		awk -v module="$module" '
			$1 == module || ($1 == "require" && $2 == module) {
				count++
				version = $1 == "require" ? $3 : $2
				kind = index($0, "// indirect") == 0 ? "direct" : "indirect"
			}
			END {
				if (count != 1) {
					exit 1
				}
				print version, kind
			}
		' go.mod
	) || {
		printf '%s\n' "$module must be declared exactly once in go.mod" >&2
		exit 1
	}

	set -- $declaration
	declared_version=$1
	declaration_kind=$2
	if [ "$declaration_kind" != direct ]; then
		printf '%s\n' "$module must be a direct dependency" >&2
		exit 1
	fi
	if [ "$declared_version" != "$expected_version" ]; then
		printf '%s\n' "$module declares $declared_version; want $expected_version" >&2
		exit 1
	fi

	resolved_version=$(go list -m -mod=readonly -f '{{.Version}}' "$module")
	if [ "$resolved_version" != "$expected_version" ]; then
		printf '%s\n' "$module resolves to $resolved_version; want $expected_version" >&2
		exit 1
	fi
done

module_graph=$(go list -m -mod=readonly all)
if printf '%s\n' "$module_graph" | grep -q -E '^k8s\.io/kubernetes([[:space:]]|$)'; then
	printf '%s\n' 'forbidden k8s.io/kubernetes module found in the resolved graph' >&2
	exit 1
fi

printf '%s\n' "verified Kubernetes modules at $expected_version"
