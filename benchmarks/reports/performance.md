# Measured operator performance

Environment: Windows/ARM64 Snapdragon X X126100 (Qualcomm Oryon), eight logical CPUs; Docker Desktop Linux/ARM64 with 7.55 GiB memory; WSL2 6.18.58.1; Go 1.27.4; Kind Kubernetes 1.37.0; Redis 7.4.7. Control plane, workers and monitoring share the same host. These are development measurements, not production capacity/SLO guarantees.

## Deterministic engine

Three native Go benchmark repetitions; median nanoseconds per operation/batch. Raw output: [engine.txt](engine.txt). Batch rows evaluate every policy in the named set, without Kubernetes/Redis I/O.

| Case | Median ns / operation or batch | Bytes / operation or batch | Allocations |
|---|---:|---:|---:|
| BenchmarkEvaluate | 158.1 | 32 | 1 |
| BenchmarkPolicySets/10 | 1521.0 | 344 | 11 |
| BenchmarkPolicySets/100 | 14697.0 | 3224 | 101 |
| BenchmarkPolicySets/500 | 72984.0 | 16024 | 501 |
| BenchmarkPolicySets/1000 | 145068.0 | 32024 | 1001 |

## Real Kubernetes/Redis dry-run pressure

Each stage creates the stated number of dry-run policies against the idle demo Deployment and queue, then measures approximately 30 seconds. Counts observed by the end include policies processed during creation. The two-second status poll update rate is a lower bound, not total reconciliation throughput. Prometheus rates/histograms use the trailing 30 seconds. Raw data: [policy-pressure.json](policy-pressure.json). Missing series are shown as unavailable.

| Policies | Observed by end | Status updates/s (sampled) | Reconciles/s | API requests/s | Redis collections/s | Queue depth | RSS MiB (largest manager) | CPU cores (both managers) | Reconcile p95 s |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 10 | 10 | 1.66 | 2.20 | 10.16 | 2.20 | 0 | 57.6 | 0.018 | 0.054 |
| 100 | 100 | 16.40 | 19.96 | 81.32 | 19.96 | 0 | 58.4 | 0.118 | 0.088 |
| 500 | 500 | 5.44 | 20.00 | 80.88 | 20.00 | 400 | 74.0 | 0.134 | 0.095 |
| 1000 | 719 | 0.79 | 20.00 | 81.04 | 20.00 | 900 | 84.4 | 0.134 | 0.095 |

The manager intentionally has bounded API-client rates and one reconcile worker. Increasing policy count can therefore grow the queue and delay convergence; not every policy is guaranteed an evaluation in the measurement window. These dry-run stages omit active ownership Leases, competing-controller checks and scale writes, and reuse one Deployment/queue/Secret. They do not demonstrate 1,000 independently scaled workloads. Longer active-policy soak tests and per-namespace partitioning remain production qualification work.

Redis collection/API-read latency series are also preserved in each [comparison JSON](hpa-comparison.md); the live dashboard exposes collection, scale-execution and controller-runtime reconciliation histograms. Microbenchmarks do not include TLS, API/network latency or scheduler startup costs.
