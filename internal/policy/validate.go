package policy

import (
	"fmt"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	"math"
	"regexp"
	"time"
	_ "time/tzdata"
)

var queuePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Validate supplements OpenAPI/CEL with IANA timezone and quantity semantics.
// Invalid objects fail closed at runtime; there is no webhook availability dependency.
func Validate(s api.WorkloadScalingPolicySpec) error {
	if s.ScaleTargetRef.Kind != "Deployment" || s.ScaleTargetRef.APIVersion != "apps/v1" || s.ScaleTargetRef.Name == "" {
		return fmt.Errorf("only named apps/v1 Deployments in the policy namespace are supported")
	}
	if s.MinReplicas < 0 || s.MaxReplicas < 1 || s.MaxReplicas > 100000 || s.MaxReplicas < s.MinReplicas {
		return fmt.Errorf("invalid replica bounds")
	}
	if s.MinReplicas == 0 && !s.Safety.AllowScaleToZero {
		return fmt.Errorf("zero minimum requires allowScaleToZero")
	}
	if s.Safety.ActivationReplicas < 1 || s.Safety.ActivationReplicas > s.MaxReplicas {
		return fmt.Errorf("invalid activation replicas")
	}
	if s.Safety.OnMetricsUnavailable != "HoldCurrent" && s.Safety.OnMetricsUnavailable != "ScaleToMinimum" {
		return fmt.Errorf("unsupported missing metrics behavior")
	}
	if s.Safety.IdleDuration.Duration < 30*time.Second || s.Safety.IdleDuration.Duration > 24*time.Hour {
		return fmt.Errorf("idle duration must be 30s..24h")
	}
	m := s.Metrics
	if !queuePattern.MatchString(m.Redis.QueueName) || m.Redis.SecretRef.Name == "" || m.Redis.ConsumerGroup == "" {
		return fmt.Errorf("invalid Redis queue configuration")
	}
	if m.TargetQueueDepthPerReplica <= 0 || m.TargetOldestJobAge.Duration <= 0 || m.TargetProcessingLatency.Duration <= 0 {
		return fmt.Errorf("metric thresholds must be positive")
	}
	if m.TargetDrainTime != nil && m.TargetDrainTime.Duration <= 0 {
		return fmt.Errorf("drain time must be positive")
	}
	if m.WorkerThroughput != nil && (*m.WorkerThroughput <= 0 || math.IsNaN(*m.WorkerThroughput) || math.IsInf(*m.WorkerThroughput, 0)) {
		return fmt.Errorf("worker throughput must be finite and positive")
	}
	if m.Redis.CollectionInterval.Duration < time.Second || m.Redis.CollectionInterval.Duration > 5*time.Minute || m.Redis.Timeout.Duration < 100*time.Millisecond || m.Redis.Timeout.Duration > 30*time.Second || m.MaxSampleAge.Duration < time.Second || m.MaxSampleAge.Duration > time.Hour {
		return fmt.Errorf("invalid sampling durations")
	}
	b := s.Behavior
	if b.Tolerance < 0 || b.Tolerance > .5 || math.IsNaN(b.Tolerance) || b.MinimumInterval.Duration < time.Second || b.MinimumInterval.Duration > time.Hour || b.StabilizationWindow.Duration < 0 || b.StabilizationWindow.Duration > 24*time.Hour {
		return fmt.Errorf("invalid hysteresis/stabilization configuration")
	}
	if b.StabilizationWindow.Duration/m.Redis.CollectionInterval.Duration > 2000 {
		return fmt.Errorf("stabilization requires >2000 samples; increase collection interval")
	}
	for _, d := range []api.DirectionBehavior{b.ScaleUp, b.ScaleDown} {
		if d.Cooldown.Duration < 0 || d.Cooldown.Duration > 24*time.Hour || d.MaxPodsPerStep < 1 || d.MaxPercentPerStep < 1 || d.MaxPercentPerStep > 1000 {
			return fmt.Errorf("invalid direction behavior")
		}
	}
	for _, q := range []*resource.Quantity{s.ResourcePolicy.MaxTotalCPU, s.ResourcePolicy.MaxTotalMemory, s.ResourcePolicy.ReservedCPU, s.ResourcePolicy.ReservedMemory} {
		if q != nil && q.Sign() < 0 {
			return fmt.Errorf("resource quantities must not be negative")
		}
	}
	if s.ResourcePolicy.ReplicaCeiling != nil && *s.ResourcePolicy.ReplicaCeiling < 0 {
		return fmt.Errorf("replica ceiling cannot be negative")
	}
	for _, w := range s.Schedules {
		if _, err := time.LoadLocation(w.TimeZone); err != nil {
			return fmt.Errorf("schedule %s has invalid IANA zone", w.Name)
		}
		start, e1 := time.Parse("15:04", w.StartTime)
		end, e2 := time.Parse("15:04", w.EndTime)
		if e1 != nil || e2 != nil || start.Equal(end) || len(w.Days) == 0 || w.MinReplicas < 0 || w.MinReplicas > s.MaxReplicas || (w.MaxReplicas != nil && (*w.MaxReplicas < w.MinReplicas || *w.MaxReplicas > s.MaxReplicas)) {
			return fmt.Errorf("schedule %s has invalid window/bounds", w.Name)
		}
		for _, day := range w.Days {
			found := false
			for d := time.Sunday; d <= time.Saturday; d++ {
				if day == d.String() {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("invalid schedule day %s", day)
			}
		}
	}
	return nil
}
