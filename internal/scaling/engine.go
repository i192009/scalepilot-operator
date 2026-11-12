// Package scaling contains deterministic decisions without Kubernetes/Redis I/O.
package scaling

import (
	"fmt"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"math"
	"time"
)

type Input struct {
	Policy                          api.WorkloadScalingPolicySpec
	Status                          api.WorkloadScalingPolicyStatus
	Metrics                         *api.QueueStatus
	Current, Ready, ResourceCeiling int32
	Now                             time.Time
}
type Decision struct {
	Recommended, Desired, Minimum, Maximum int32
	Schedule, Reason                       string
	Limited                                bool
	History                                []api.Recommendation
	IdleSince                              *metav1.MicroTime
}

func ValidSample(m *api.QueueStatus, now time.Time, maxAge time.Duration) error {
	if m == nil {
		return fmt.Errorf("sample missing")
	}
	age := now.Sub(m.CollectedAt.Time)
	if age > maxAge || age < -time.Second {
		return fmt.Errorf("sample stale or future dated")
	}
	if m.Pending < 0 || m.InFlight < 0 || m.ExecutionSamples < 0 || m.Pending > math.MaxInt64-m.InFlight {
		return fmt.Errorf("invalid queue counts")
	}
	for _, n := range []float64{m.OldestAgeSeconds, m.ArrivalRate, m.CompletionRate, m.FailureRate, m.AverageProcessingSeconds, m.P95ProcessingSeconds} {
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return fmt.Errorf("invalid metric")
		}
	}
	if m.ExecutionSamples > 0 && (m.AverageProcessingSeconds <= 0 || m.P95ProcessingSeconds <= 0) {
		return fmt.Errorf("execution samples missing latency")
	}
	return nil
}
func Evaluate(in Input) (Decision, error) {
	s := in.Policy
	lo, hi, schedule, err := ScheduleBounds(s, in.Now)
	if err != nil {
		return Decision{}, err
	}
	requiredMin := lo
	hi = min(hi, in.ResourceCeiling)
	lo = min(lo, hi)
	d := Decision{Desired: in.Current, Recommended: in.Current, Minimum: lo, Maximum: hi, Schedule: schedule, Reason: "WithinTolerance", IdleSince: in.Status.IdleSince}
	if err := ValidSample(in.Metrics, in.Now, s.Metrics.MaxSampleAge.Duration); err != nil {
		d.Reason = "MetricsUnavailable"
		d.Limited = true
		d.IdleSince = nil
		d.History = []api.Recommendation{{Time: metav1.NewMicroTime(in.Now), Replicas: in.Current}}
		// ScaleToMinimum is a fail-safe floor: never authorizes a metrics-blind downscale.
		if s.Safety.OnMetricsUnavailable == "ScaleToMinimum" && in.Current < lo {
			d.Recommended = lo
			step := max(1, min(s.Behavior.ScaleUp.MaxPodsPerStep, ceil(float64(max(1, in.Current))*float64(s.Behavior.ScaleUp.MaxPercentPerStep)/100)))
			d.Desired = min(lo, in.Current+step)
			last := safetyTime(in.Status)
			if last != nil && in.Now.Sub(last.Time) < max(s.Behavior.ScaleUp.Cooldown.Duration, s.Behavior.MinimumInterval.Duration) {
				d.Desired = in.Current
				d.Reason = "CooldownActive"
			}
		}
		if s.Paused || s.DryRun {
			d.Desired = in.Current
		}
		return d, nil
	}
	if hi < requiredMin {
		d.Reason = "ResourceBudgetUnsatisfiable"
		d.Limited = true
		d.History = in.Status.Recommendations
		return d, nil
	}
	m := in.Metrics
	work := float64(m.Pending) + float64(m.InFlight)
	candidate := ceil(float64(m.Pending) / float64(s.Metrics.TargetQueueDepthPerReplica))
	rate := 0.0
	if m.ExecutionSamples > 0 {
		rate = 1 / m.AverageProcessingSeconds
	} else if s.Metrics.WorkerThroughput != nil {
		rate = *s.Metrics.WorkerThroughput
	}
	if rate > 0 {
		if s.Metrics.TargetDrainTime != nil {
			candidate = max(candidate, ceil((work/s.Metrics.TargetDrainTime.Seconds()+m.ArrivalRate)/rate))
		}
		// Remaining age budget shrinks continuously; once overdue use one sample period.
		if m.Pending > 0 {
			remaining := max(s.Metrics.Redis.CollectionInterval.Seconds(), s.Metrics.TargetOldestJobAge.Seconds()-m.OldestAgeSeconds)
			candidate = max(candidate, ceil((float64(m.Pending)/remaining+m.ArrivalRate)/rate))
		}
	}
	// A serial worker's measured p95 service time estimates conservative arrival capacity.
	// Execution latency is NOT queue wait; a slow CPU job alone is not proof more replicas help.
	if m.ExecutionSamples > 0 && m.Pending > 0 && m.P95ProcessingSeconds > s.Metrics.TargetProcessingLatency.Seconds() {
		candidate = max(candidate, ceil(m.ArrivalRate*m.P95ProcessingSeconds))
	}
	// One serial worker per active job. Never remove workers solely because unread lag is zero.
	candidate = max(candidate, min(in.Current, ceil(float64(m.InFlight))))
	if work == 0 && m.ArrivalRate == 0 {
		if d.IdleSince == nil {
			v := metav1.NewMicroTime(in.Now)
			d.IdleSince = &v
		}
		if s.Safety.AllowScaleToZero && lo == 0 {
			if in.Now.Sub(d.IdleSince.Time) < s.Safety.IdleDuration.Duration {
				candidate = max(candidate, 1)
			}
		}
	} else {
		d.IdleSince = nil
		if in.Current == 0 {
			candidate = max(candidate, s.Safety.ActivationReplicas)
		}
	}
	candidate = max(lo, min(hi, candidate))
	d.Recommended = candidate
	window := s.Behavior.StabilizationWindow.Duration
	// Seed current capacity on first observation, so a new/restarted policy cannot skip its low-demand window.
	if len(in.Status.Recommendations) == 0 && window > 0 {
		d.History = append(d.History, api.Recommendation{Time: metav1.NewMicroTime(in.Now), Replicas: in.Current})
	}
	for _, p := range in.Status.Recommendations {
		if in.Now.Sub(p.Time.Time) <= window {
			d.History = append(d.History, p)
		}
	}
	// Watches do not increase durable history cardinality: one point per collection period,
	// retaining the maximum recommendation in each period.
	if len(d.History) > 0 && in.Now.Sub(d.History[len(d.History)-1].Time.Time) < s.Metrics.Redis.CollectionInterval.Duration {
		last := &d.History[len(d.History)-1]
		last.Replicas = max(last.Replicas, candidate)
	} else {
		d.History = append(d.History, api.Recommendation{Time: metav1.NewMicroTime(in.Now), Replicas: candidate})
	}
	desired := candidate
	if desired < in.Current {
		for _, h := range d.History {
			desired = max(desired, h.Replicas)
		}
		desired = min(desired, hi)
		if desired != candidate {
			d.Reason = "Stabilizing"
		}
	}
	if desired < in.Current && in.Ready < in.Current {
		desired = in.Current
		d.Reason = "CapacityNotReady"
	}
	// Persisted intent also acts as a timestamp when a process died before acknowledgement.
	last := safetyTime(in.Status)
	if desired != in.Current && last != nil {
		cooldown := s.Behavior.ScaleUp.Cooldown.Duration
		if desired < in.Current {
			cooldown = s.Behavior.ScaleDown.Cooldown.Duration
		}
		if in.Now.Sub(last.Time) < max(cooldown, s.Behavior.MinimumInterval.Duration) {
			desired = in.Current
			d.Reason = "CooldownActive"
		}
	}
	if desired != in.Current && in.Current >= lo && in.Current <= hi && math.Abs(float64(desired-in.Current))/float64(max(1, in.Current)) <= s.Behavior.Tolerance {
		desired = in.Current
		d.Reason = "WithinTolerance"
	}
	if desired > in.Current {
		limit := s.Behavior.ScaleUp
		step := max(1, min(limit.MaxPodsPerStep, ceil(float64(max(1, in.Current))*float64(limit.MaxPercentPerStep)/100)))
		desired = min(desired, in.Current+step)
		d.Reason = "DemandScaleUp"
	}
	if desired < in.Current {
		limit := s.Behavior.ScaleDown
		step := max(1, min(limit.MaxPodsPerStep, int32(math.Floor(float64(in.Current)*float64(limit.MaxPercentPerStep)/100))))
		desired = max(desired, in.Current-step)
		if d.Reason != "Stabilizing" {
			d.Reason = "LowDemandScaleDown"
		}
	}
	if s.Paused {
		desired = in.Current
		d.Reason = "Paused"
	}
	if s.DryRun {
		desired = in.Current
		d.Reason = "DryRun"
	}
	d.Desired = max(0, desired)
	d.Limited = d.Desired != d.Recommended || in.ResourceCeiling < s.MaxReplicas
	return d, nil
}
func ceil(n float64) int32 {
	if n >= 100000 {
		return 100000
	}
	if n <= 0 {
		return 0
	}
	return int32(math.Ceil(n))
}

func safetyTime(s api.WorkloadScalingPolicyStatus) *metav1.MicroTime {
	last := s.LastScaleTime
	if s.LastScaleAttemptTime != nil && (last == nil || s.LastScaleAttemptTime.After(last.Time)) {
		last = s.LastScaleAttemptTime
	}
	if p := s.PendingScale; p != nil && (last == nil || p.Time.After(last.Time)) {
		last = &p.Time
	}
	return last
}
