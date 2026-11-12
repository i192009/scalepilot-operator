# ADR 2: durable safety and exclusive replica admission

Accepted. Persist recommendation history, timestamps and pre-write intents in policy status. Recovered ambiguous intents conservatively consume cooldown; this prioritizes avoiding repeated unsafe writes over avoiding an unnecessary delay.

Use a policy-owned target Lease with compare-and-swap and direct API conflict checks. Enable admission guarding replica-changing full Deployment and scale updates. Runtime checks alone have a check/write race against independent controllers, and server-side apply does not provide a replica lock. Admission trades scaling-write availability for fail-closed exclusivity. HPA creation is allowed so conflict state and recovery can be demonstrated, but competing writes are denied.

No finalizer is needed: the policy owns only a garbage-collected Lease, and deleting a policy must not change application replicas or Redis data.
