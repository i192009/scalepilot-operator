# Exclusive scaling ownership

The default Helm installation enables the replica admission guard. A policy exclusively owns a same-namespace Deployment while it is neither paused nor dry-run. Do not disable this guard in a supported deployment. Runtime checks alone cannot atomically exclude other Kubernetes writers.

1. Uncached API reads detect all HPAs (including KEDA-created HPAs) and other active policies targeting the workload. Every conflicting active policy suspends scaling.
2. A target-UID-derived Lease is acquired with atomic create or resourceVersion compare-and-swap. Its holder is the policy UID, with a policy ownerReference. It does not expire by time: a stalled controller cannot lose its fence to a second policy. Takeover requires the owner to be absent or explicitly inactive.
3. Status persists the intended `from`, `to`, target UID, and decision time before the scale write.
4. Immediately before every bounded retry the executor rechecks policy UID/generation, deletion, pause/dry-run, target UID, conflicts, ownership, and current replicas. It updates only `/scale` with optimistic concurrency.
5. The admission webhook rejects other identities changing replicas through `/scale` or a full Deployment update while any active policy exists. It rejects even the operator when there are duplicate policies, an HPA conflict, or no matching durable intent.

HPA creation itself is allowed. The HPA's competing replica writes are denied, and ScalePilot reports ConflictDetected, emits an Event, and stops its own writes. Delete the HPA or pause/delete the ScalePilot policy to transfer control. This avoids two active replica writers during supported operation. The guard does not use server-side-apply field ownership as a lock.

If the webhook cannot be reached, replica-changing updates fail closed in its selected namespace. Unchanged replica updates bypass the webhook using a CEL matchCondition. This trades scaling-write availability for exclusivity; run both admission replicas on reliable nodes in production. An administrator can remove admission/RBAC or impersonate the service account, so this is not a boundary against cluster administrators. Do not share the operator service account with applications.

For emergency handoff, first set `spec.paused: true` and verify the status generation. The guard then permits the next owner's replica writes. Policy deletion never changes the current count or deletes the workload/queue. The original application owner must establish subsequent replica management.

Helm generates a private CA and server certificate for local installation and retains the existing TLS Secret on upgrade. Certificates last 365 days. Rotate them deliberately before expiry: remove the webhook configuration during a controlled scaling freeze, delete its TLS Secret, then upgrade/reinstall the chart to generate a matching CA/serving pair. Automated cert-manager rotation is not included. Do not commit generated TLS Secrets.
