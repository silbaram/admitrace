package kube136

// EquivalentSubresourceSupported reports whether Kubernetes 1.36 can return
// targetSubresource from an equivalence lookup for requestSubresource.
// Kubernetes keys equivalent resources by the requested subresource and does
// not perform cross-subresource conversion.
func EquivalentSubresourceSupported(requestSubresource, targetSubresource string) bool {
	return requestSubresource == targetSubresource
}
