package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	State       = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "scalepilot_policy", Help: "Policy state and capacity; namespace/name labels bounded by policy count."}, []string{"namespace", "policy", "measure"})
	Operations  = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scalepilot_scale_operations_total", Help: "Acknowledged scale writes."}, []string{"namespace", "policy", "direction"})
	Blocked     = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scalepilot_decisions_blocked_total", Help: "Evaluations limited by policy."}, []string{"reason"})
	Collection  = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "scalepilot_collection_duration_seconds", Help: "Redis collection latency including credential lookup.", Buckets: prometheus.DefBuckets})
	APIDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "scalepilot_scale_api_duration_seconds", Help: "Scale execution latency including ownership checks and bounded retries.", Buckets: prometheus.DefBuckets})
	Errors      = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scalepilot_errors_total", Help: "Failures by bounded category."}, []string{"category"})
)

func init() {
	metrics.Registry.MustRegister(State, Operations, Blocked, Collection, APIDuration, Errors)
}
func Publish(p *api.WorkloadScalingPolicy) {
	s := p.Status
	values := map[string]float64{"active": 1, "current": float64(s.CurrentReplicas), "ready": float64(s.ReadyReplicas), "desired": float64(s.DesiredReplicas), "recommended": float64(s.RecommendedReplicas), "minimum": float64(s.EffectiveMinReplicas), "maximum": float64(s.EffectiveMaxReplicas)}
	if s.LastEvaluationTime != nil {
		values["last_evaluation"] = float64(s.LastEvaluationTime.Unix())
	}
	if q := s.Queue; q != nil {
		values["pending"] = float64(q.Pending)
		values["inflight"] = float64(q.InFlight)
		values["oldest_age_seconds"] = q.OldestAgeSeconds
		values["arrival_rate"] = q.ArrivalRate
		values["completion_rate"] = q.CompletionRate
		values["failure_rate"] = q.FailureRate
		values["processing_average_seconds"] = q.AverageProcessingSeconds
		values["processing_p95_seconds"] = q.P95ProcessingSeconds
		values["sample_timestamp"] = float64(q.CollectedAt.Unix())
	}
	for _, c := range s.Conditions {
		n := 0.0
		if c.Status == "True" {
			n = 1
		}
		values[c.Type] = n
	}
	for k, v := range values {
		State.WithLabelValues(p.Namespace, p.Name, k).Set(v)
	}
}
func Delete(namespace, name string) {
	State.DeletePartialMatch(prometheus.Labels{"namespace": namespace, "policy": name})
	Operations.DeletePartialMatch(prometheus.Labels{"namespace": namespace, "policy": name})
}
