package testutil

import (
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

func Duration(d time.Duration) metav1.Duration { return metav1.Duration{Duration: d} }
func Spec() api.WorkloadScalingPolicySpec {
	rate := 2.0
	drain := Duration(120 * time.Second)
	return api.WorkloadScalingPolicySpec{
		ScaleTargetRef: api.TargetReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "worker"}, MinReplicas: 1, MaxReplicas: 20,
		Metrics:  api.ScalingMetrics{Redis: api.RedisSource{SecretRef: api.SecretReference{Name: "redis"}, QueueName: "jobs", ConsumerGroup: "workers", CollectionInterval: Duration(5 * time.Second), Timeout: Duration(3 * time.Second)}, TargetQueueDepthPerReplica: 100, TargetOldestJobAge: Duration(30 * time.Second), TargetProcessingLatency: Duration(2 * time.Second), TargetDrainTime: &drain, WorkerThroughput: &rate, MaxSampleAge: Duration(30 * time.Second)},
		Behavior: api.ScalingBehavior{ScaleUp: api.DirectionBehavior{Cooldown: Duration(10 * time.Second), MaxPodsPerStep: 5, MaxPercentPerStep: 100}, ScaleDown: api.DirectionBehavior{Cooldown: Duration(30 * time.Second), MaxPodsPerStep: 2, MaxPercentPerStep: 25}, StabilizationWindow: Duration(60 * time.Second), MinimumInterval: Duration(5 * time.Second), Tolerance: .1},
		Safety:   api.SafetyPolicy{OnMetricsUnavailable: "HoldCurrent", ActivationReplicas: 1, IdleDuration: Duration(300 * time.Second)},
	}
}
