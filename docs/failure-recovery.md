# Recovery and failure injection

```mermaid
sequenceDiagram
  participant Old as Old leader
  participant API as Kubernetes API
  participant New as Standby manager
  Old->>API: Persist recommendation history + scale intent
  Old->>API: Optimistic update /scale
  Note over Old: Process terminates
  New->>API: Acquire manager-election Lease
  New->>API: Read policy status + actual replicas
  New->>API: Recheck ownership and resource constraints
  Note over New: Respect persisted cooldown; no blind intent replay
  New->>API: Resume bounded reconciliation
```

| Failure | Response | Executable evidence |
|---|---|---|
| Redis outage or stale metrics | Hold current replicas, reset idle/low-demand evidence, MetricsAvailable=False | envtest outage; Kind TestRedisOutage |
| API resource-version conflict | Bounded backoff/re-read; preserve unrelated Deployment fields | envtest TestOptimisticConflictRetriesPreserveFields |
| Manager crash | New elected leader reads durable status; ambiguous intent delays rather than repeats scaling | envtest recovery; Kind TestFailover |
| Worker SIGKILL | Unacknowledged delivery remains in the PEL; XAUTOCLAIM reclaims it | real Redis integration; Kind TestWorkerCrashRecovery |
| Worker SIGTERM | Stop claiming; allow a bounded grace period; leave unfinished work recoverable | worker implementation; rollout/Pod-termination scenarios |
| Metric spike | Finite-value checks, replica/resource ceilings and per-step limits | engine unit/property tests |
| HPA/duplicate policy | Both policy/controller checks and admission deny competing writes | envtest conflict/duplicate; Kind TestConflict/admission test |
| Unschedulable Pods | Requested and ready counts differ; Degraded=True; no false capacity-success claim | Kind TestResourceBudgetAndUnavailableCapacity |

Redis uses a PVC with AOF `everysec` for the local demo. Worker crash recovery does not imply a guarantee against Redis disk loss: AOF may lose about one second of acknowledged writes after a Redis host failure. Redis failover/replication, backups and exactly-once execution are outside scope. The effect-idempotency marker has a 24-hour retention period; external business effects require their own durable transaction/idempotency boundary.

The Kind suite collects Pods, events, policy status and operator logs under `artifacts/` on failure. `make test-e2e` mutates the dedicated demo and requires an installed operator; envtest is a separate API/etcd test, not a full Kubernetes cluster. `make test-conflict` and `make test-failover` select focused Kind tests.
