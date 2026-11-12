# Resource safeguards and capacity

`ResourceCeiling` computes exact rational divisions of the available CPU/memory workload budgets by per-Pod requests. ReservedCPU/ReservedMemory are subtracted first. Fractional Kubernetes quantities are not converted through floating point. Each enabled budget requires explicit nonzero requests on each relevant container unless Pod-level requests provide the named resource.

Requests include regular container sums, restartable init sidecars, the maximum sequential init stage with preceding sidecars, Pod-level requests where supplied, and Pod overhead. CPU and memory ceilings intersect with maxReplicas and optional replicaCeiling.

Example: 500m requested per worker with a 4-core budget permits at most eight replicas. Adding a 500m reserve permits seven. A 1Gi total memory budget with 128Mi per worker permits eight. The tighter limit wins.

These are configured workload budgets, not a claim about physical cluster capacity. ScalePilot does not sum node allocatable resources as a substitute for Kubernetes scheduling. Affinity, taints, competing workloads, volume constraints and fragmentation can still leave Pods Pending. Deployment ready replicas and kube-state-metrics Pending Pod panels expose this difference. The controller marks incomplete readiness degraded and conservatively holds downscale.

Changing a budget below already requested capacity does not force-delete Pods. Existing capacity converges under cooldown, readiness/in-flight protection and rate limits; an infeasible minimum/budget combination holds and reports degradation. Namespace ResourceQuotas and LimitRanges remain the appropriate admission safeguards. Namespace free-capacity prediction/reservation is not implemented.
