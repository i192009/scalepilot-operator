# WorkloadScalingPolicy API (v1alpha1)

API group `autoscaling.scalepilot.io`; kind `WorkloadScalingPolicy`; plural `workloadscalingpolicies`; short name `wsp`. Go types and generator markers are authoritative. `make generate manifests` generates DeepCopy, OpenAPI/CEL CRDs and RBAC and synchronizes chart copies. Do not independently edit generated schemas.

| Path | Meaning / default |
|---|---|
| scaleTargetRef | apps/v1 Deployment name; policy namespace only |
| minReplicas / maxReplicas | Required bounds; max 1..100000, min >= 0 |
| metrics.redis.secretRef.name | Same-namespace Secret containing `address` (host:port) |
| metrics.redis.queueName | 1..64 alphanumeric/underscore/hyphen characters |
| metrics.redis.consumerGroup | `workers`; this queue protocol permits one group |
| metrics.redis.collectionInterval | `5s`, valid 1s..5m |
| metrics.redis.timeout | `3s`, valid 100ms..30s |
| metrics.redis.tls / serverName | Enable verified TLS 1.2+; override certificate server name |
| metrics.targetQueueDepthPerReplica | Required positive number of unread jobs per replica |
| metrics.targetOldestJobAge | `30s`, positive |
| metrics.targetProcessingLatency | `2s`, positive execution-latency overload threshold |
| metrics.targetDrainTime | Optional positive backlog drain horizon |
| metrics.workerThroughput | Optional positive per-worker jobs/second bootstrap rate |
| metrics.maxSampleAge | `30s`, valid 1s..1h |
| behavior.scaleUp / scaleDown | Required cooldown, maxPodsPerStep, maxPercentPerStep |
| behavior.stabilizationWindow | `300s`, 0..24h; downscale history window |
| behavior.minimumInterval | `5s`, valid 1s..1h |
| behavior.tolerance | `0.1`, valid 0..0.5 |
| schedules | Up to 32 named local-time windows; priority and min/max overrides |
| resourcePolicy | Optional CPU/memory budgets, reserves and replicaCeiling |
| safety.onMetricsUnavailable | `HoldCurrent`; optional `ScaleToMinimum` availability floor |
| safety.allowScaleToZero | false; required when minimum is zero |
| safety.activationReplicas | 1, cannot exceed maximum |
| safety.idleDuration | `300s`, valid 30s..24h |
| paused / dryRun | false; both suppress replica writes |

Secret keys: required `address`; optional `username`, `password`, `ca.crt`, `tls.crt`, `tls.key`. Both certificate/key are required for mTLS. The address must not embed credentials. Secrets are fetched directly, not stored in the CRD/status/logs. Pool connections rotate when Secret data or TLS settings change.

Kubernetes quantity parsing uses API machinery, including millicores and binary memory units. Schema/CEL reject malformed quantities, durations, bounds, unsupported kinds and invalid time strings. The controller additionally validates IANA timezones, schedule relationships and positive quantities. Invalid configuration is marked degraded and cannot scale. This avoids requiring a policy-validation webhook solely for timezone databases.

Status includes observedGeneration, current/ready/desired/recommended replicas, effective bounds, queue snapshot/time, evaluation/scale/direction timestamps, idle start, next evaluation, schedule, reason, durable recommendation history, pending scale intent and standard metav1.Conditions. Current means observed requested replicas; desired means the permitted decision; recommended means constrained raw demand. They can differ during API writes, readiness delay, cooldown or dry-run. Status is observational and must not be edited by application users.

History is capped at 2048 points. Runtime validation rejects combinations needing more than 2000 collection periods per stabilization window. Early workload events cannot create an unbounded history. Public float metrics are finite validated numeric values; controller-gen's allowDangerousTypes is deliberate for measurable fractional rates/latencies, not untyped arbitrary objects.

See [the functional policy](../deployments/demo/30-policy.yaml). To use a schedule add:

```yaml
schedules:
  - name: office
    timeZone: Europe/London
    days: [Monday, Tuesday, Wednesday, Thursday, Friday]
    startTime: "09:00"
    endTime: "18:00"
    minReplicas: 3
    maxReplicas: 20
    priority: 10
```

Helm installs CRDs from `crds/` on first install; it does not reliably upgrade existing CRDs. Apply the new generated CRDs explicitly before a controller/chart upgrade. v1alpha1 has no conversion webhook or backwards-compatibility promise. Back up policies/status and review schema changes before production upgrades.
