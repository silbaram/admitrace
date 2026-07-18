#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ -z "${KUBEBUILDER_ASSETS:-}" ]; then
	printf '%s\n' 'KUBEBUILDER_ASSETS must point to exact Kubernetes 1.36.2 envtest assets' >&2
	exit 2
fi

cd "$project_dir/conformance"
GOWORK=off GOPROXY=off go run -mod=readonly ./cmd/resourcecatalog \
	-verify ../internal/compat/kube136/resource_catalog.json
printf '%s\n' 'verified Kubernetes 1.36.2 resource catalog'
