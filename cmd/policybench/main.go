// Command policybench measures real dry-run reconciliation pressure at increasing
// policy counts. It creates no application Pods and makes no scale writes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"github.com/scalepilot/scalepilot-operator/internal/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Policies              int
	MeasuredAt            time.Time
	DurationSeconds       float64
	ObservedPolicies      int
	ObservedStatusUpdates int
	UpdatesPerSecond      float64
	Prometheus            map[string]json.RawMessage
}

func main() {
	var stages, endpoint, output string
	var duration time.Duration
	flag.StringVar(&stages, "policies", "10,100,500,1000", "stage policy counts")
	flag.DurationVar(&duration, "duration", 30*time.Second, "measurement window after each creation stage")
	flag.StringVar(&endpoint, "prometheus", "http://127.0.0.1:9090", "Prometheus HTTP API")
	flag.StringVar(&output, "output", "benchmarks/reports/policy-pressure.json", "actual measurements")
	flag.Parse()
	selectedContext, contextErr := exec.Command("kubectl", "config", "current-context").Output()
	must(contextErr)
	if !strings.HasPrefix(strings.TrimSpace(string(selectedContext)), "kind-") {
		panic("policy pressure tests require an isolated Kind context")
	}
	if duration < time.Second {
		panic("invalid duration")
	}
	config := ctrl.GetConfigOrDie()
	config.QPS = 100
	config.Burst = 100
	scheme := runtime.NewScheme()
	must(api.AddToScheme(scheme))
	c, e := client.New(config, client.Options{Scheme: scheme})
	must(e)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	labels := map[string]string{"scalepilot.io/experiment": "policy-pressure"}
	selector := client.MatchingLabels(labels)
	defer func() {
		_ = c.DeleteAllOf(context.Background(), &api.WorkloadScalingPolicy{}, client.InNamespace("demo"), selector)
	}()
	var results []Result
	for _, stage := range strings.Split(stages, ",") {
		count, e := strconv.Atoi(stage)
		must(e)
		if count < 1 || count > 1000 {
			panic("counts must be 1..1000")
		}
		must(c.DeleteAllOf(ctx, &api.WorkloadScalingPolicy{}, client.InNamespace("demo"), selector))
		for j := range count {
			s := testutil.Spec()
			s.ScaleTargetRef.Name = "document-worker"
			s.Metrics.Redis.SecretRef.Name = "redis-connection"
			s.Metrics.Redis.QueueName = "document-jobs"
			s.DryRun = true
			p := &api.WorkloadScalingPolicy{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("pressure-%04d", j), Namespace: "demo", Labels: labels}, Spec: s}
			must(c.Create(ctx, p))
		}
		start := time.Now()
		rv := map[string]string{}
		updates := 0
		for time.Since(start) < duration {
			var list api.WorkloadScalingPolicyList
			must(c.List(ctx, &list, client.InNamespace("demo"), selector))
			for _, p := range list.Items {
				if p.Status.LastEvaluationTime != nil {
					if previous := rv[p.Name]; previous != "" && previous != p.ResourceVersion {
						updates++
					}
					rv[p.Name] = p.ResourceVersion
				}
			}
			time.Sleep(2 * time.Second)
		}
		record := Result{Policies: count, MeasuredAt: start.UTC(), DurationSeconds: time.Since(start).Seconds(), ObservedPolicies: len(rv), ObservedStatusUpdates: updates, UpdatesPerSecond: float64(updates) / time.Since(start).Seconds(), Prometheus: map[string]json.RawMessage{}}
		for name, expr := range map[string]string{"reconciliations_per_second": "sum(rate(controller_runtime_reconcile_total{controller=\"workloadscalingpolicy\"}[30s]))", "queue_depth": "sum(workqueue_depth)", "redis_collections_per_second": "sum(rate(scalepilot_collection_duration_seconds_count[30s]))", "api_requests_per_second": "sum(rate(rest_client_requests_total{job=\"scalepilot\"}[30s]))", "resident_memory_bytes": "max(process_resident_memory_bytes{job=\"scalepilot\"})", "cpu_cores": "sum(rate(process_cpu_seconds_total{job=\"scalepilot\"}[30s]))", "reconcile_p95_seconds": "histogram_quantile(0.95,sum by(le)(rate(controller_runtime_reconcile_time_seconds_bucket[30s])))"} {
			value, err := query(ctx, endpoint, expr)
			must(err)
			record.Prometheus[name] = value
		}
		results = append(results, record)
		b, _ := json.MarshalIndent(results, "", "  ")
		must(os.WriteFile(output, b, 0644))
		fmt.Printf("%d policies: %d observed, %.2f observed status updates/s\n", count, len(rv), record.UpdatesPerSecond)
	}
}
func query(ctx context.Context, endpoint, expr string) (json.RawMessage, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v1/query?query="+url.QueryEscape(expr), nil)
	if e != nil {
		return nil, e
	}
	c := http.Client{Timeout: 10 * time.Second}
	response, e := c.Do(req)
	if e != nil {
		return nil, e
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus HTTP %d", response.StatusCode)
	}
	var result struct {
		Status string
		Data   json.RawMessage
	}
	if e = json.NewDecoder(response.Body).Decode(&result); e != nil {
		return nil, e
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed")
	}
	return result.Data, nil
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}
