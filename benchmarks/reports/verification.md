# Executed verification

Verified locally on 12 November 2026, Windows/ARM64 with Docker Desktop Linux/ARM64, eight CPUs and 7.55 GiB Docker memory. The dedicated two-node Kind cluster runs Kubernetes 1.37.0. Go/tool versions are recorded in [environment.txt](environment.txt). No GitHub-hosted workflow or external release publication is claimed.

| Check | Result | Evidence / limits |
|---|---|---|
| Native Go builds | Passed for manager, worker, loadgen, benchmark and policybench | [build.log](build.log); Linux-native output directory used to avoid a Windows bind-mount copy failure |
| All three Docker image targets | Built and deployed to Kind | Final-build IDs in images.json; multi-platform release builds have not run |
| Formatting, go vet, golangci-lint 2.15.0 | Passed | [static-race.log](static-race.log), final-regression.log |
| Go unit/property tests | Passed | [final-regression.log](final-regression.log); deterministic timestamps, bounds, unavailable metrics, oscillation, schedules/DST, resource quantities, admission and fractional cooldown JSON persistence |
| Race detector | Passed on internal and command packages | [static-race.log](static-race.log); not a claim of race-testing a whole distributed cluster |
| envtest | All eight tests passed | Real API server/etcd 1.37.0; status, scale, invalid schema/configuration, duplicates/HPA, pause/dry-run/deletion, recovery and optimistic conflict retry; no running Pods |
| Redis Testcontainers integration | Both scenarios passed | [redis-integration.log](redis-integration.log): real lag/PEL/recovery/idempotency/dead letters/outage and unknown-lag fallback; authenticated mutual TLS, hostname verification, malformed CA and invalid password rejection |
| Kind acceptance | All seven tests passed in **260.120 s** on final timestamp build | [e2e.log](e2e.log): queue scale/cooldown/drain; HPA conflict/event/resume; Redis outage/recovery; leader failover; worker interruption/reclamation; resource budgets/unavailable Pods; competing-writer admission rejection |
| Generated DeepCopy/CRD/RBAC reproducibility | Passed | [generation.log](generation.log); regenerated files compared against the Git index |
| Helm lint/template | Passed; chart installed/upgraded successfully | [helm.log](helm.log); default two-replica, same-namespace deployment actually ran |
| Workflow syntax | actionlint 1.7.12 passed | [workflows.log](workflows.log); GitHub jobs themselves have not run |
| govulncheck 1.9.0 | No reachable vulnerabilities found | [vulnerabilities.txt](vulnerabilities.txt); one imported-package finding was not reachable according to the tool |
| Trivy 0.76.2 | No HIGH or CRITICAL findings in all three images | trivy-operator.json, trivy-worker.json, trivy-loadgen.json; scanner/database timing matters |
| Monitoring | Actual metrics scraped and live Grafana graphs rendered | monitoring-health.json and [real screenshots](../../docs/images/); Grafana 12.4.15 with SQLite WAL; explicit browser locale |
| Secured metrics | HTTPS anonymous access rejected (401); authorized monitoring identity accepted (200) | [secure-metrics.txt](secure-metrics.txt); self-signed certificate trust bypass used only for this local endpoint check; default HTTP monitoring restored afterward |
| Shell/PowerShell helper syntax | Passed | Bash syntax checks and PowerShell parser check executed locally |
| Full comparison matrix | All 22 arms completed: 3,540 jobs, zero failed | [Measured table and raw audits](hpa-comparison.md): repeated I/O/CPU spikes, steady/ramp/burst/short overload, long jobs, Redis outage, worker crash and actual controller restarts |
| Controller recovery after comparison faults | Passed | [restart-verification.json](restart-verification.json): new running Kubernetes controller-manager container, new ready ScalePilot leader, Grafana healthy and all five Prometheus targets up; controller topologies differ |
| Regression after benchmark harness changes | Passed | [benchmark-regression.log](benchmark-regression.log): lint/vet, unit/race, both real Redis scenarios, all eight envtest tests, five builds, Helm, generated-file comparison, shell syntax and repeated Metrics Server setup |

The API's safety timestamps were refined to microsecond precision before the final full Kind run. Earlier benchmark sets under exploratory/seconds-timestamps use the previous build and are retained only as historical evidence. Final reports use the separate rerun and its recorded image IDs.

Known validation limits: no public registry/GitHub release, no release-workflow execution, no alternate architecture/CNI/platform E2E, no sustained 1,000-active-workload soak/SLO qualification, and no real OTLP backend trace-delivery test. Redis TLS/auth is tested against real Redis; a fully deployed Kubernetes demo with encrypted Redis remains separate production qualification. The Kind default CNI does not enforce NetworkPolicy, so manifest presence does not prove isolation.

The tested `v1alpha1` API is an initial development API with no conversion/backwards-compatibility guarantee. Development-only status was reset once during the pre-release timestamp-format refinement while the demo was idle. Ordinary restart/failover does not reset status and is covered by the executed tests. Production adoption needs controlled schema/certificate upgrades, authenticated monitoring, HA Redis/storage and backups.
