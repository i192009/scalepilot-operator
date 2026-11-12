# ScalePilot engineering guide

This repository was scaffolded with Kubebuilder go/v4 and uses controller-runtime. Helm is the canonical manager/admission deployment package; config/crd and config/rbac contain generator output. This guide replaces Kubebuilder's generic scaffold notes with the actual project workflow.

## Boundaries and invariants

- api/v1alpha1: API types and generator markers. Use API machinery only here.
- internal/scaling: deterministic decisions, no Redis/Kubernetes I/O, explicit timestamps.
- internal/queue: single-group Streams protocol and measured worker telemetry.
- internal/metrics: Secret/TLS resolution and shared connection pools.
- internal/workload: direct ownership checks, target Leases and optimistic /scale writes.
- internal/security: custom replica admission handler for Deployment and scale updates. This specialized handler covers core/external subresources rather than a generated CRD-validation webhook.
- internal/controller: bounded reconciliation, durable intent/status and watches.
- tests/envtest: real API/etcd; tests/integration: real Redis; tests/e2e: dedicated Kind only.

Never treat missing metrics as zero, current requested replicas as ready capacity, or process-local memory as durable cooldown. Keep competing replica writers excluded by the default-on admission guard plus runtime checks and target Lease. Do not claim exactly-once execution or cluster scheduling guarantees.

## Generation and scaffold maintenance

Use Kubebuilder CLI for new API kinds. Preserve PROJECT metadata and the +kubebuilder:scaffold markers in cmd/main.go. Do not manually edit zz_generated files, generated CRD schemas or config/rbac/role.yaml. Edit Go markers and run make generate manifests. The same target copies generated schemas/rules into the chart.

## Required checks after changes

After Go changes run make lint-fix and make test (requires Docker and envtest assets). On Docker Desktop inside the development container, use TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal for the Redis integration component. For focused iteration use make test-unit / test-envtest / test-integration. Run make test-race for changes affecting concurrent or shared state. After controller/execution/deployment changes run the relevant Kind tests and make helm-lint.

E2E mutates only the dedicated kind-scalepilot context and demo namespace. Never run it on an unrelated dev/production cluster. It shortens policy windows; restore deployments/demo/30-policy.yaml afterward. Preserve failure diagnostics under artifacts (gitignored) and actual successful reports under benchmarks/reports. Do not invent benchmark results, successful workflow runs or screenshots.

The PowerShell wrapper scripts/dev.ps1 supplies Go/Make/Helm/Kind inside Docker. See docs/deployment.md for setup and actual commands. No host installation or external publication is needed for normal verification. Releases require explicit dispatch; publishing is disabled by default.
