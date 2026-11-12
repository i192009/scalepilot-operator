# Measured CPU-HPA comparison

Generated from completed job-audit JSON on 2026-11-12T11:03:55Z. These are local observations, not a general superiority claim.

Audited across 22 arms: 3540 completed jobs, 0 failed jobs.

| Run / raw data | Strategy | Jobs | Configured mean ms | Drain s | Reaction s | p95 wait s | Mean execution s | p95 execution s | Peak replicas | Pod-seconds | Completed / failed | Mean CPU cores | Mean memory MiB | Collection failures |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| [hpa-burst-io-1.json](hpa-burst-io-1.json) | hpa | 100 | 400 | 41.97 | not observed | 9.50 | 0.40 | 0.50 | 1 | 41.90 | 100/0 | 0.003 | 3.0 | 0 |
| [pilot-burst-io-1.json](pilot-burst-io-1.json) | pilot | 100 | 400 | 41.33 | not observed | 9.54 | 0.40 | 0.50 | 1 | 41.24 | 100/0 | 0.002 | 2.3 | 0 |
| [hpa-controller-restart-io-1.json](hpa-controller-restart-io-1.json) | hpa | 200 | 400 | 81.40 | not observed | 57.18 | 0.40 | 0.49 | 1 | 81.25 | 200/0 | 0.003 | 2.7 | 0 |
| [pilot-controller-restart-io-1.json](pilot-controller-restart-io-1.json) | pilot | 200 | 400 | 34.08 | 8.55 | 12.31 | 0.40 | 0.49 | 4 | 91.86 | 200/0 | 0.002 | 2.5 | 0 |
| [hpa-overload-io-1.json](hpa-overload-io-1.json) | hpa | 100 | 400 | 41.10 | not observed | 37.73 | 0.40 | 0.50 | 1 | 41.00 | 100/0 | 0.002 | 2.4 | 0 |
| [pilot-overload-io-1.json](pilot-overload-io-1.json) | pilot | 100 | 400 | 23.83 | 5.36 | 21.43 | 0.40 | 0.50 | 2 | 43.27 | 100/0 | 0.004 | 3.3 | 0 |
| [hpa-ramp-io-1.json](hpa-ramp-io-1.json) | hpa | 100 | 400 | 43.25 | not observed | 33.49 | 0.40 | 0.50 | 1 | 42.25 | 100/0 | 0.003 | 2.7 | 0 |
| [pilot-ramp-io-1.json](pilot-ramp-io-1.json) | pilot | 100 | 400 | 25.01 | 6.34 | 17.36 | 0.40 | 0.50 | 2 | 44.65 | 100/0 | 0.001 | 3.4 | 0 |
| [hpa-redis-outage-io-1.json](hpa-redis-outage-io-1.json) | hpa | 200 | 400 | 94.31 | not observed | 57.92 | 0.40 | 0.49 | 1 | 94.26 | 200/0 | 0.005 | 3.5 | 0 |
| [pilot-redis-outage-io-1.json](pilot-redis-outage-io-1.json) | pilot | 200 | 400 | 42.25 | 9.75 | 23.14 | 0.40 | 0.49 | 6 | 150.14 | 200/0 | 0.007 | 4.6 | 0 |
| [hpa-spike-cpu-1.json](hpa-spike-cpu-1.json) | hpa | 250 | 400 | 38.71 | 18.12 | 37.07 | 0.40 | 0.50 | 6 | 116.42 | 250/0 | 0.916 | 8.4 | 0 |
| [pilot-spike-cpu-1.json](pilot-spike-cpu-1.json) | pilot | 250 | 400 | 32.55 | 4.34 | 30.36 | 0.40 | 0.50 | 6 | 118.10 | 250/0 | 0.606 | 6.6 | 0 |
| [hpa-spike-io-1.json](hpa-spike-io-1.json) | hpa | 250 | 400 | 102.42 | not observed | 96.26 | 0.40 | 0.50 | 1 | 102.36 | 250/0 | 0.005 | 3.0 | 0 |
| [pilot-spike-io-1.json](pilot-spike-io-1.json) | pilot | 250 | 400 | 32.45 | 4.37 | 29.96 | 0.40 | 0.50 | 6 | 119.62 | 250/0 | 0.007 | 4.2 | 0 |
| [hpa-spike-io-2.json](hpa-spike-io-2.json) | hpa | 250 | 400 | 103.07 | not observed | 96.18 | 0.40 | 0.50 | 1 | 103.02 | 250/0 | 0.004 | 3.0 | 0 |
| [pilot-spike-io-2.json](pilot-spike-io-2.json) | pilot | 250 | 400 | 32.64 | 4.35 | 30.16 | 0.40 | 0.50 | 6 | 120.16 | 250/0 | 0.005 | 2.8 | 0 |
| [hpa-spike-long-1.json](hpa-spike-long-1.json) | hpa | 20 | 3000 | 64.65 | not observed | 57.78 | 3.18 | 3.75 | 1 | 64.59 | 20/0 | 0.001 | 2.2 | 0 |
| [pilot-spike-long-1.json](pilot-spike-long-1.json) | pilot | 20 | 3000 | 27.02 | 5.33 | 22.50 | 3.18 | 3.75 | 4 | 75.72 | 20/0 | 0.003 | 3.1 | 0 |
| [hpa-steady-io-1.json](hpa-steady-io-1.json) | hpa | 100 | 400 | 40.91 | not observed | 19.31 | 0.40 | 0.50 | 1 | 40.86 | 100/0 | 0.002 | 2.9 | 0 |
| [pilot-steady-io-1.json](pilot-steady-io-1.json) | pilot | 100 | 400 | 28.71 | 13.75 | 7.66 | 0.40 | 0.50 | 2 | 44.62 | 100/0 | 0.002 | 2.5 | 0 |
| [hpa-worker-crash-io-1.json](hpa-worker-crash-io-1.json) | hpa | 200 | 400 | 83.04 | not observed | 58.57 | 0.40 | 0.49 | 1 | 82.97 | 200/0 | 0.004 | 2.8 | 0 |
| [pilot-worker-crash-io-1.json](pilot-worker-crash-io-1.json) | pilot | 200 | 400 | 34.71 | 9.75 | 13.65 | 0.40 | 0.49 | 4 | 91.00 | 200/0 | 0.003 | 2.7 | 0 |

Configuration: job counts and configured mean durations are listed per row, with seeded ±25% variation, one initial replica, six maximum, identical worker image and 100m CPU/32Mi memory requests with one-core/128Mi limits. Each arm warms for 30 seconds. HPA uses a realistic 60% CPU target. Arms run sequentially (HPA first), with independent queues. The repeated I/O and CPU spike subset uses 250 jobs at 400ms; the additional matrix is defined in [benchmark-matrix.sh](../../scripts/benchmark-matrix.sh). See each raw file's timestamps, sample series, nullable resource data and collection-failure count.

Environment: Windows/ARM64 Snapdragon X X126100 (Qualcomm Oryon), eight logical CPUs; Docker Desktop Linux/ARM64 with 7.55 GiB memory, WSL2 6.18.58.1 kernel; Go 1.27.4; two-node Kind Kubernetes 1.37.0; Redis 7.4.7. Monitoring and the control plane are colocated. Image IDs are in [images.json](images.json); tool details are in [environment.txt](environment.txt).

Limitations: small local runs; fixed arm ordering; approximate one-second sampling plus API/collection time; Metrics Server rolling-window CPU; no isolated physical hosts or statistical significance claim. Pod-seconds stop at queue drain and exclude a long post-drain downscale tail. No KEDA/custom-metric-HPA comparison ran. Historical exploratory results under exploratory/ used prior builds or measurement-harness revisions and are excluded.

See [methodology](../../docs/hpa-comparison.md). The observed peak/scaling response can favor different controllers for different CPU/I/O profiles; these experiments do not establish a production cost or latency advantage.

Resource sampling: CPU/memory values sum only Pod metrics currently available from Metrics Server. New Pods can lack samples, so these observed sums can underrepresent total workload usage. They are not complete cost totals and do not demonstrate CPU efficiency. Queue/drain, replica and completion-audit measurements are independent of this limitation.

Faults actually injected:

- hpa-controller-restart-io-1.json: stopped Kind kube-controller-manager CRI container 39c437af47c4201afaa9bf0c9cf9312f9e3596a92b99da96f4f0e8e12232892c; kubelet restarts the static Pod (also affects other built-in controllers); producer retries: 0.
- pilot-controller-restart-io-1.json: force-deleted active ScalePilot leader scalepilot-controller-7466758976-clfp2 (two manager replicas); producer retries: 0.
- hpa-redis-outage-io-1.json: Redis scaled to zero for 10 seconds, then restored; includes restart time; producer retries: 7.
- pilot-redis-outage-io-1.json: Redis scaled to zero for 10 seconds, then restored; includes restart time; producer retries: 7.
- hpa-worker-crash-io-1.json: force-deleted this arm's worker Pods at >=10 seconds; producer retries: 0.
- pilot-worker-crash-io-1.json: force-deleted this arm's worker Pods at >=10 seconds; producer retries: 0.

Controller restarts affect different topologies: HPA runs within the single Kind kube-controller-manager process; ScalePilot uses two replicas with leader election. This is a recovery demonstration, not an equivalent availability SLO comparison. Redis fault injection blocks the sampler during the 10-second outage and a 10-second restart allowance; collection-failure counts omit that deliberate gap. Pod-seconds across a sampling gap use the next observed replica count, so failure-run resource integrals are approximate. Producers retry the same job ID for at most one minute; completion effects remain idempotent under ambiguous enqueue outcomes.
