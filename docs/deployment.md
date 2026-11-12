# Local deployment and operation

## Prerequisites

Docker with a Linux engine, Go **1.27.4**, GNU Make, Bash, curl and tar. The bootstrap script installs pinned Kind 0.33.0, kubectl 1.37.1, Helm 4.3.1 and controller-gen 0.23.0. The two-node Kind cluster uses Kubernetes 1.37.0's official release digest. Allow several GB of Docker memory; this verification environment exposes 7.55 GiB and eight CPUs.

The repository was initially generated with Kubebuilder 4.12.0's go/v4 plugin and then updated to the verified runtime/API versions. Kubebuilder is not needed to build or deploy the committed project. The PROJECT file preserves the scaffold metadata.

## Linux/macOS

```bash
export PATH="$HOME/.local/bin:$PATH"
TOOL_BIN="$HOME/.local/bin" bash scripts/bootstrap-tools.sh
make generate manifests fmt vet test-unit
make kind-create docker-build kind-load install-crds deploy deploy-demo
make monitoring-up
```

The operator starts with two replicas and owns only namespace `demo`. Redis persists local AOF on a Kind PVC. Workload installation does not generate traffic automatically.

```bash
make generate-load
kubectl -n demo get wsp -w
kubectl -n demo get deployment document-worker -w
kubectl -n demo get events --sort-by=.lastTimestamp
```

The explicit load Job enqueues 1,000 500ms jobs. Expect requested capacity to increase within step/cooldown limits and ready capacity to follow as Pods start. After draining, the 60-second recommendation stabilization window and 30-second downscale cooldown produce gradual reduction to one worker. Do not equate a requested increase with ready workers.

Open separate terminals for monitoring:

```bash
kubectl -n monitoring port-forward svc/grafana 3000:3000
kubectl -n monitoring port-forward svc/prometheus 9090:9090
```

Visit `http://localhost:3000/d/scalepilot/scalepilot-workload-autoscaling` and `http://localhost:9090`. Grafana 12.4.15 provisions its dashboard and datasource automatically; local anonymous access is Viewer-only. SQLite WAL is enabled. Cold image pulls/provisioning can take several minutes. Monitoring retention is six hours and ephemeral. Grafana 13.3.1 was evaluated but its bundled plugins failed with a browser initialization error in this environment; the maintained 12.4 patch line is pinned for this demo.

## Windows PowerShell using Docker tooling

No host Go/Git/Make/Helm installation is necessary. Run from the repository root with Docker Desktop's Linux engine active:

If script execution is restricted, define `function spdev { powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev.ps1 @args }` and use `spdev` in place of `.\scripts\dev.ps1` below. This changes only the child process's script-execution setting, not machine policy, and was verified on this host.

```powershell
.\scripts\dev.ps1 setup
.\scripts\dev.ps1 make kind-create
.\scripts\dev.ps1 connect-kind
.\scripts\dev.ps1 make docker-build kind-load install-crds deploy deploy-demo monitoring-up
.\scripts\dev.ps1 make generate-load
.\scripts\dev.ps1 make test-unit test-envtest
.\scripts\dev.ps1 make test-e2e
.\scripts\dev.ps1 export-kubeconfig
```

The development container joins the Kind Docker network and uses its internal API endpoint. Host tools must use the exported external endpoint:

```powershell
kubectl --kubeconfig .kube/config -n demo get wsp -w
kubectl --kubeconfig .kube/config -n monitoring port-forward svc/grafana 3000:3000
# In another terminal:
kubectl --kubeconfig .kube/config -n monitoring port-forward svc/prometheus 9090:9090
```

For integration tests run inside the development container on Docker Desktop:

```powershell
.\scripts\dev.ps1 env TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal make test-integration
```

This env override is not needed on ordinary native Linux. Docker socket access gives the development container control of the local Docker engine; use it only for trusted project development.

For native executable output in this Windows/Docker setup, `.\scripts\dev.ps1 make build BIN_DIR=/tmp/scalepilot-bin` avoids large ELF writes through Docker Desktop's Windows file-sharing layer. That layer returned a memory-allocation error during one large CLI copy in this environment; native Linux container paths and Docker image builds succeeded.

## Tests and experiments

```bash
make test-unit test-race test-envtest
make test-integration
make test-e2e
make test-conflict test-failover
make benchmark
```

E2E requires the deployed demo. It mutates only namespace demo on the `kind-scalepilot` context by default and intentionally shortens demo safety windows. Reapply `deployments/demo/30-policy.yaml` after running it to restore the documented demonstration settings. Failure diagnostics are written to artifacts/. Envtest is API server/etcd only; Kind runs actual Pods and leader election.

For equivalent HPA comparisons, ensure no other experiment is running. In another terminal run `kubectl -n demo port-forward svc/redis 16379:6379`, then:

```bash
make benchmark-hpa
make benchmark-matrix
go run ./cmd/benchmark --scenario=spike --job-type=cpu --jobs=250 --job-duration=400ms --repeats=3
go run ./cmd/benchmark --scenario=steady --rate=5 --jobs=250
go run ./cmd/benchmark --scenario=ramp --rate=5 --jobs=250
go run ./cmd/benchmark --scenario=burst --rate=5 --jobs=250
```

Both benchmark Make targets install pinned Metrics Server and a Kind-only insecure-kubelet-TLS flag. `make benchmark-matrix` manages its own reconnecting Redis port-forward; the other CLI commands need the manual forward above. The benchmark CLI creates isolated queues and sequential HPA/ScalePilot arms with equivalent worker resources; results go to benchmarks/reports. Runtime injection variants also support `redis-outage`, `worker-crash` and `controller-restart`. Set --job-type=long/fast/medium/io and a corresponding --job-duration for other workload profiles. The CLI rejects incomplete job audits rather than reporting made-up successful drains.

Measure policy cardinality separately, while the demo queue is idle and Prometheus is port-forwarded:

```bash
go run ./cmd/policybench --policies=10,100,500,1000 --duration=30s
```

This is real dry-run reconciliation pressure against Kubernetes/Redis, not a production capacity certification. It does not create 1,000 worker Deployments or test 1,000 active ownership/scaling loops. Raw Prometheus results preserve missing series as empty results.

## Optional actual screenshot capture

Port-forward Grafana inside the development container on `--address=0.0.0.0`, then run the pinned browser container on the Kind Docker network:

```powershell
docker exec scalepilot-dev kubectl -n monitoring port-forward svc/grafana 3000:3000 --address=0.0.0.0
# Another terminal:
docker run --rm --network kind -v "${PWD}:/workspace" -w /workspace mcr.microsoft.com/playwright:v1.58.2-noble bash -c 'npm install --prefix /tmp/scalepilot-capture playwright@1.58.2 --no-audit --no-fund && node scripts/capture-dashboard.mjs'
```

Node/Playwright is optional screenshot tooling, not an operator dependency. The screenshot script checks real Grafana loading and query responses; it never generates synthetic dashboard images.

To rebuild the selected results gallery from recorded benchmark JSON and verification evidence, replace `node scripts/capture-dashboard.mjs` in that container command with `node scripts/capture-results.mjs`. It writes the report captures, selected Grafana copies and provenance manifest to `screenshots/`. The reports use the existing dark Grafana palette and link back to the actual measurement files. See the [gallery](../screenshots/README.md).

## Production configuration and cleanup

Helm is the canonical deployment package. Generated CRDs/RBAC remain under config/. Use fixed published image tags, authenticated Redis TLS, an enforcing CNI, protected monitoring and reliable admission serving/certificate rotation. Enable secure metrics only with a matching authenticated scraper configuration. See security.md and ownership.md.

```bash
helm lint charts/scalepilot
helm template scalepilot charts/scalepilot --namespace scalepilot-system
kubectl apply --server-side --field-manager=scalepilot-crds --force-conflicts -f config/crd/bases
helm upgrade --install scalepilot charts/scalepilot --namespace scalepilot-system --create-namespace
```

CRDs require explicit upgrade application; Helm does not automatically upgrade them from crds/. The installation target explicitly makes the generated ScalePilot schema authoritative with its own field manager, including taking over Helm's initial CRD field ownership. Review schema changes before doing this in an existing cluster. For a planned ownership handoff, pause/delete policies before uninstalling the admission guard. Policy deletion itself preserves application resources and data.

`make undeploy` explicitly removes the operator and demo manifests, including the demo PVC. `make kind-delete` or scripts/cleanup.sh deletes the dedicated Kind cluster and its local persistent data. Do not run destructive demo cleanup against a production environment. Development Docker cache volumes remain for subsequent builds; remove them separately only if no longer wanted.
