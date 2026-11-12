//go:build ignore

// Run explicitly: go run scripts/report-benchmarks.go
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type report struct {
	Fault                                                                                      string
	ProducerRetries                                                                            int64
	Strategy, Scenario, JobType                                                                string
	Jobs                                                                                       int
	MeanDurationMS                                                                             int64
	Started                                                                                    time.Time
	DurationSeconds                                                                            float64
	ReactionSeconds                                                                            *float64
	MaxBacklog                                                                                 int64
	MaxReplicas                                                                                int32
	PodSeconds, ReadyPodSeconds                                                                float64
	ScaleUps, ScaleDowns, Oscillations, Completed, Failed, CollectionFailures, ResourceSamples int
	AverageWaitSeconds, P95WaitSeconds, AverageProcessingSeconds, P95ProcessingSeconds         float64
	AverageCPUCore, AverageMemoryBytes                                                         *float64
}

func main() {
	files, e := filepath.Glob("benchmarks/reports/hpa-*.json")
	must(e)
	pilot, e := filepath.Glob("benchmarks/reports/pilot-*.json")
	must(e)
	files = append(files, pilot...)
	sort.Slice(files, func(i, j int) bool {
		profile := func(path string) string {
			return strings.TrimPrefix(strings.TrimPrefix(filepath.Base(path), "hpa-"), "pilot-")
		}
		if profile(files[i]) == profile(files[j]) {
			return files[i] < files[j]
		}
		return profile(files[i]) < profile(files[j])
	})
	var rows []string
	var faults []string
	completed, failed := 0, 0
	for _, file := range files {
		data, e := os.ReadFile(file)
		must(e)
		var r report
		must(json.Unmarshal(data, &r))
		completed += r.Completed
		failed += r.Failed
		if r.Fault != "" {
			faults = append(faults, fmt.Sprintf("- %s: %s; producer retries: %d.", filepath.Base(file), r.Fault, r.ProducerRetries))
		}
		reaction := "not observed"
		if r.ReactionSeconds != nil {
			reaction = fmt.Sprintf("%.2f", *r.ReactionSeconds)
		}
		cpu, mem := "unavailable", "unavailable"
		if r.AverageCPUCore != nil {
			cpu = fmt.Sprintf("%.3f", *r.AverageCPUCore)
		}
		if r.AverageMemoryBytes != nil {
			mem = fmt.Sprintf("%.1f", *r.AverageMemoryBytes/1048576)
		}
		rows = append(rows, fmt.Sprintf("| [%s](%s) | %s | %d | %d | %.2f | %s | %.2f | %.2f | %.2f | %d | %.2f | %d/%d | %s | %s | %d |", filepath.Base(file), filepath.Base(file), r.Strategy, r.Jobs, r.MeanDurationMS, r.DurationSeconds, reaction, r.P95WaitSeconds, r.AverageProcessingSeconds, r.P95ProcessingSeconds, r.MaxReplicas, r.PodSeconds, r.Completed, r.Failed, cpu, mem, r.CollectionFailures))
	}
	if len(rows) == 0 {
		panic("no completed comparison files; refusing to manufacture results")
	}
	text := fmt.Sprintf("# Measured CPU-HPA comparison\n\nGenerated from completed job-audit JSON on %s. These are local observations, not a general superiority claim.\n\n", time.Now().UTC().Format(time.RFC3339))
	text += fmt.Sprintf("Audited across %d arms: %d completed jobs, %d failed jobs.\n\n", len(rows), completed, failed)
	text += "| Run / raw data | Strategy | Jobs | Configured mean ms | Drain s | Reaction s | p95 wait s | Mean execution s | p95 execution s | Peak replicas | Pod-seconds | Completed / failed | Mean CPU cores | Mean memory MiB | Collection failures |\n|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n" + strings.Join(rows, "\n") + "\n\n"
	text += "Configuration: job counts and configured mean durations are listed per row, with seeded ±25% variation, one initial replica, six maximum, identical worker image and 100m CPU/32Mi memory requests with one-core/128Mi limits. Each arm warms for 30 seconds. HPA uses a realistic 60% CPU target. Arms run sequentially (HPA first), with independent queues. The repeated I/O and CPU spike subset uses 250 jobs at 400ms; the additional matrix is defined in [benchmark-matrix.sh](../../scripts/benchmark-matrix.sh). See each raw file's timestamps, sample series, nullable resource data and collection-failure count.\n\n"
	text += "Environment: Windows/ARM64 Snapdragon X X126100 (Qualcomm Oryon), eight logical CPUs; Docker Desktop Linux/ARM64 with 7.55 GiB memory, WSL2 6.18.58.1 kernel; Go 1.27.4; two-node Kind Kubernetes 1.37.0; Redis 7.4.7. Monitoring and the control plane are colocated. Image IDs are in [images.json](images.json); tool details are in [environment.txt](environment.txt).\n\n"
	text += "Limitations: small local runs; fixed arm ordering; approximate one-second sampling plus API/collection time; Metrics Server rolling-window CPU; no isolated physical hosts or statistical significance claim. Pod-seconds stop at queue drain and exclude a long post-drain downscale tail. No KEDA/custom-metric-HPA comparison ran. Historical exploratory results under exploratory/ preceded the queue-lag fix and are excluded.\n\nSee [methodology](../../docs/hpa-comparison.md). The observed peak/scaling response can favor different controllers for different CPU/I/O profiles; these experiments do not establish a production cost or latency advantage.\n"
	text = strings.ReplaceAll(text, "preceded the queue-lag fix", "used prior builds or measurement-harness revisions")
	text += "\nResource sampling: CPU/memory values sum only Pod metrics currently available from Metrics Server. New Pods can lack samples, so these observed sums can underrepresent total workload usage. They are not complete cost totals and do not demonstrate CPU efficiency. Queue/drain, replica and completion-audit measurements are independent of this limitation.\n"
	if len(faults) > 0 {
		text += "\nFaults actually injected:\n\n" + strings.Join(faults, "\n") + "\n\nController restarts affect different topologies: HPA runs within the single Kind kube-controller-manager process; ScalePilot uses two replicas with leader election. This is a recovery demonstration, not an equivalent availability SLO comparison. Redis fault injection blocks the sampler during the 10-second outage and a 10-second restart allowance; collection-failure counts omit that deliberate gap. Pod-seconds across a sampling gap use the next observed replica count, so failure-run resource integrals are approximate. Producers retry the same job ID for at most one minute; completion effects remain idempotent under ambiguous enqueue outcomes.\n"
	}
	must(os.WriteFile("benchmarks/reports/hpa-comparison.md", []byte(text), 0644))
	performance()
}

func performance() {
	raw, e := os.ReadFile("benchmarks/reports/engine.txt")
	must(e)
	type batch struct {
		values        []float64
		bytes, allocs string
	}
	samples := map[string]*batch{}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 8 || !strings.HasPrefix(f[0], "Benchmark") || f[3] != "ns/op" {
			continue
		}
		name := f[0][:strings.LastIndex(f[0], "-")]
		n, e := strconv.ParseFloat(f[2], 64)
		must(e)
		if samples[name] == nil {
			samples[name] = &batch{bytes: f[4], allocs: f[6]}
		}
		samples[name].values = append(samples[name].values, n)
	}
	names := make([]string, 0, len(samples))
	for n := range samples {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := strings.Split(names[i], "/"), strings.Split(names[j], "/")
		if len(a) == 2 && len(b) == 2 && a[0] == b[0] {
			x, _ := strconv.Atoi(a[1])
			y, _ := strconv.Atoi(b[1])
			return x < y
		}
		return names[i] < names[j]
	})
	text := "# Measured operator performance\n\nEnvironment: Windows/ARM64 Snapdragon X X126100 (Qualcomm Oryon), eight logical CPUs; Docker Desktop Linux/ARM64 with 7.55 GiB memory; WSL2 6.18.58.1; Go 1.27.4; Kind Kubernetes 1.37.0; Redis 7.4.7. Control plane, workers and monitoring share the same host. These are development measurements, not production capacity/SLO guarantees.\n\n## Deterministic engine\n\nThree native Go benchmark repetitions; median nanoseconds per operation/batch. Raw output: [engine.txt](engine.txt). Batch rows evaluate every policy in the named set, without Kubernetes/Redis I/O.\n\n| Case | Median ns / operation or batch | Bytes / operation or batch | Allocations |\n|---|---:|---:|---:|\n"
	for _, n := range names {
		s := samples[n]
		sort.Float64s(s.values)
		text += fmt.Sprintf("| %s | %.1f | %s | %s |\n", n, s.values[len(s.values)/2], s.bytes, s.allocs)
	}
	var stages []struct {
		Policies              int
		DurationSeconds       float64
		ObservedPolicies      int
		ObservedStatusUpdates int
		UpdatesPerSecond      float64
		Prometheus            map[string]json.RawMessage
	}
	raw, e = os.ReadFile("benchmarks/reports/policy-pressure.json")
	must(e)
	must(json.Unmarshal(raw, &stages))
	text += "\n## Real Kubernetes/Redis dry-run pressure\n\nEach stage creates the stated number of dry-run policies against the idle demo Deployment and queue, then measures approximately 30 seconds. Counts observed by the end include policies processed during creation. The two-second status poll update rate is a lower bound, not total reconciliation throughput. Prometheus rates/histograms use the trailing 30 seconds. Raw data: [policy-pressure.json](policy-pressure.json). Missing series are shown as unavailable.\n\n| Policies | Observed by end | Status updates/s (sampled) | Reconciles/s | API requests/s | Redis collections/s | Queue depth | RSS MiB (largest manager) | CPU cores (both managers) | Reconcile p95 s |\n|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n"
	for _, s := range stages {
		m := s.Prometheus
		text += fmt.Sprintf("| %d | %d | %.2f | %s | %s | %s | %s | %s | %s | %s |\n", s.Policies, s.ObservedPolicies, s.UpdatesPerSecond, metric(m["reconciliations_per_second"], 1, 2), metric(m["api_requests_per_second"], 1, 2), metric(m["redis_collections_per_second"], 1, 2), metric(m["queue_depth"], 1, 0), metric(m["resident_memory_bytes"], 1048576, 1), metric(m["cpu_cores"], 1, 3), metric(m["reconcile_p95_seconds"], 1, 3))
	}
	text += "\nThe manager intentionally has bounded API-client rates and one reconcile worker. Increasing policy count can therefore grow the queue and delay convergence; not every policy is guaranteed an evaluation in the measurement window. These dry-run stages omit active ownership Leases, competing-controller checks and scale writes, and reuse one Deployment/queue/Secret. They do not demonstrate 1,000 independently scaled workloads. Longer active-policy soak tests and per-namespace partitioning remain production qualification work.\n\nRedis collection/API-read latency series are also preserved in each [comparison JSON](hpa-comparison.md); the live dashboard exposes collection, scale-execution and controller-runtime reconciliation histograms. Microbenchmarks do not include TLS, API/network latency or scheduler startup costs.\n"
	must(os.WriteFile("benchmarks/reports/performance.md", []byte(text), 0644))
}
func metric(raw json.RawMessage, divisor float64, decimals int) string {
	var data struct {
		Result []struct{ Value []json.RawMessage }
	}
	if json.Unmarshal(raw, &data) != nil || len(data.Result) == 0 || len(data.Result[0].Value) < 2 {
		return "unavailable"
	}
	var value string
	if json.Unmarshal(data.Result[0].Value[1], &value) != nil {
		return "unavailable"
	}
	n, e := strconv.ParseFloat(value, 64)
	if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return "unavailable"
	}
	return strconv.FormatFloat(n/divisor, 'f', decimals, 64)
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}
