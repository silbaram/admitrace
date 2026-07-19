# Documentation

English | [한국어](README.ko.md)

AdmiTrace documentation is separated by purpose. Product guides explain how the CLI behaves and how to use it. Testing guides explain how contributors validate changes and reproduce release evidence. Commands in these documents run from the repository root unless stated otherwise.

## Product guides

- [Quickstart](product/quickstart.md): build the CLI, explain manifests, use limited live hydration, and export or replay snapshots.
- [Scenario, manifest adapter, and result reference](product/reference.md): input and result contracts, routing semantics, reason codes, exit codes, limitations, and non-goals.

## Testing and validation

- [Test environment setup](testing/test-environment-setup.md): choose between local regression tests, pinned Kubernetes `1.36.2` envtest, and optional Docker+kind end-to-end tests.
- [Release readiness](testing/release-readiness.md): run the fail-closed release gate and understand its evidence.
- [Public beta validation](../validation/beta/README.md): inspect the pinned Gatekeeper and Istio validation cases.

## Executable examples

- [`examples/`](examples): Scenario fixtures for `admitrace test`.
- [`manifest-examples/`](manifest-examples): Kubernetes resources, Namespace objects, and WebhookConfigurations for manifest explanation.
