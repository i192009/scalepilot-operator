package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TargetReference is deliberately same-namespace only.
type TargetReference struct {
	// +kubebuilder:validation:Enum=apps/v1
	// +kubebuilder:default="apps/v1"
	APIVersion string `json:"apiVersion,omitempty"`
	// +kubebuilder:validation:Enum=Deployment
	// +kubebuilder:default=Deployment
	Kind string `json:"kind,omitempty"`
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}
type SecretReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}
type RedisSource struct {
	// Secret keys: address; optional username,password,ca.crt,tls.crt,tls.key.
	SecretRef SecretReference `json:"secretRef"`
	// +kubebuilder:validation:Pattern="^[a-zA-Z0-9_-]{1,64}$"
	QueueName string `json:"queueName"`
	// +kubebuilder:default=workers
	// +kubebuilder:validation:MinLength=1
	ConsumerGroup string `json:"consumerGroup,omitempty"`
	// +kubebuilder:default="5s"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1s') && duration(self) <= duration('5m')",message="interval must be 1s..5m"
	CollectionInterval metav1.Duration `json:"collectionInterval,omitempty"`
	// +kubebuilder:default="3s"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('100ms') && duration(self) <= duration('30s')",message="timeout must be 100ms..30s"
	Timeout    metav1.Duration `json:"timeout,omitempty"`
	TLS        bool            `json:"tls,omitempty"`
	ServerName string          `json:"serverName,omitempty"`
}
type ScalingMetrics struct {
	Redis RedisSource `json:"redis"`
	// +kubebuilder:validation:Minimum=1
	TargetQueueDepthPerReplica int64 `json:"targetQueueDepthPerReplica"`
	// +kubebuilder:default="30s"
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s')",message="age must be positive"
	TargetOldestJobAge metav1.Duration `json:"targetOldestJobAge,omitempty"`
	// +kubebuilder:default="2s"
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s')",message="latency must be positive"
	TargetProcessingLatency metav1.Duration `json:"targetProcessingLatency,omitempty"`
	// +optional
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s')",message="drain time must be positive"
	TargetDrainTime *metav1.Duration `json:"targetDrainTime,omitempty"`
	// Per-worker jobs/second fallback when execution measurements do not exist.
	// +optional
	// +kubebuilder:validation:Minimum=0.001
	WorkerThroughput *float64 `json:"workerThroughput,omitempty"`
	// +kubebuilder:default="30s"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1s') && duration(self) <= duration('1h')",message="max sample age must be 1s..1h"
	MaxSampleAge metav1.Duration `json:"maxSampleAge,omitempty"`
}
type DirectionBehavior struct {
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('0s') && duration(self) <= duration('24h')",message="cooldown must be 0..24h"
	Cooldown metav1.Duration `json:"cooldown"`
	// +kubebuilder:validation:Minimum=1
	MaxPodsPerStep int32 `json:"maxPodsPerStep"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	MaxPercentPerStep int32 `json:"maxPercentPerStep"`
}
type ScalingBehavior struct {
	ScaleUp   DirectionBehavior `json:"scaleUp"`
	ScaleDown DirectionBehavior `json:"scaleDown"`
	// +kubebuilder:default="300s"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('0s') && duration(self) <= duration('24h')",message="window must be 0..24h"
	StabilizationWindow metav1.Duration `json:"stabilizationWindow,omitempty"`
	// +kubebuilder:default="5s"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1s') && duration(self) <= duration('1h')",message="minimum interval must be 1s..1h"
	MinimumInterval metav1.Duration `json:"minimumInterval,omitempty"`
	// +kubebuilder:default=0.1
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=0.5
	Tolerance float64 `json:"tolerance,omitempty"`
}

// Highest priority wins; equal priorities use lexical name. Overnight windows
// belong to the day on which they start. End is exclusive.
// +kubebuilder:validation:XValidation:rule="self.startTime != self.endTime",message="start and end must differ"
// +kubebuilder:validation:XValidation:rule="!has(self.maxReplicas) || self.maxReplicas >= self.minReplicas",message="schedule max must be >= min"
type Schedule struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MaxLength=128
	TimeZone string `json:"timeZone"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=7
	// +kubebuilder:validation:items:Enum=Sunday;Monday;Tuesday;Wednesday;Thursday;Friday;Saturday
	Days []string `json:"days"`
	// +kubebuilder:validation:Pattern="^([01][0-9]|2[0-3]):[0-5][0-9]$"
	// +kubebuilder:validation:MaxLength=5
	StartTime string `json:"startTime"`
	// +kubebuilder:validation:Pattern="^([01][0-9]|2[0-3]):[0-5][0-9]$"
	// +kubebuilder:validation:MaxLength=5
	EndTime string `json:"endTime"`
	// +kubebuilder:validation:Minimum=0
	MinReplicas int32 `json:"minReplicas"`
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`
	Priority    int32  `json:"priority,omitempty"`
}
type ResourcePolicy struct {
	// +optional
	MaxTotalCPU *resource.Quantity `json:"maxTotalCPU,omitempty"`
	// +optional
	MaxTotalMemory *resource.Quantity `json:"maxTotalMemory,omitempty"`
	// Reserved capacity is subtracted from the workload budget before division.
	// +optional
	ReservedCPU *resource.Quantity `json:"reservedCPU,omitempty"`
	// +optional
	ReservedMemory *resource.Quantity `json:"reservedMemory,omitempty"`
	// +optional
	// +kubebuilder:validation:Minimum=0
	ReplicaCeiling *int32 `json:"replicaCeiling,omitempty"`
}
type SafetyPolicy struct {
	// +kubebuilder:validation:Enum=HoldCurrent;ScaleToMinimum
	// +kubebuilder:default=HoldCurrent
	OnMetricsUnavailable string `json:"onMetricsUnavailable,omitempty"`
	AllowScaleToZero     bool   `json:"allowScaleToZero,omitempty"`
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	ActivationReplicas int32 `json:"activationReplicas,omitempty"`
	// +kubebuilder:default="300s"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('30s') && duration(self) <= duration('24h')",message="idle period must be 30s..24h"
	IdleDuration metav1.Duration `json:"idleDuration,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.maxReplicas >= self.minReplicas",message="maxReplicas must be >= minReplicas"
// +kubebuilder:validation:XValidation:rule="self.minReplicas > 0 || self.safety.allowScaleToZero",message="zero minimum requires explicit scale-to-zero"
// +kubebuilder:validation:XValidation:rule="self.safety.activationReplicas <= self.maxReplicas",message="activation exceeds maximum"
type WorkloadScalingPolicySpec struct {
	ScaleTargetRef TargetReference `json:"scaleTargetRef"`
	// +kubebuilder:validation:Minimum=0
	MinReplicas int32 `json:"minReplicas"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100000
	MaxReplicas int32           `json:"maxReplicas"`
	Metrics     ScalingMetrics  `json:"metrics"`
	Behavior    ScalingBehavior `json:"behavior"`
	// +kubebuilder:validation:MaxItems=32
	// +listType=map
	// +listMapKey=name
	Schedules      []Schedule     `json:"schedules,omitempty"`
	ResourcePolicy ResourcePolicy `json:"resourcePolicy,omitempty"`
	Safety         SafetyPolicy   `json:"safety"`
	Paused         bool           `json:"paused,omitempty"`
	DryRun         bool           `json:"dryRun,omitempty"`
}
type Recommendation struct {
	Time     metav1.MicroTime `json:"time"`
	Replicas int32            `json:"replicas"`
}

// PendingScale is saved BEFORE /scale. Its timestamp conservatively fences a
// retry after a crash between scale execution and status acknowledgement.
type PendingScale struct {
	Time      metav1.MicroTime `json:"time"`
	From      int32            `json:"from"`
	To        int32            `json:"to"`
	TargetUID string           `json:"targetUID"`
}
type QueueStatus struct {
	Pending                  int64       `json:"pending"`
	InFlight                 int64       `json:"inFlight"`
	OldestAgeSeconds         float64     `json:"oldestAgeSeconds"`
	ArrivalRate              float64     `json:"arrivalRate"`
	CompletionRate           float64     `json:"completionRate"`
	FailureRate              float64     `json:"failureRate"`
	AverageProcessingSeconds float64     `json:"averageProcessingSeconds"`
	P95ProcessingSeconds     float64     `json:"p95ProcessingSeconds"`
	ExecutionSamples         int64       `json:"executionSamples"`
	CollectedAt              metav1.Time `json:"collectedAt"`
}
type WorkloadScalingPolicyStatus struct {
	ObservedGeneration   int64             `json:"observedGeneration,omitempty"`
	CurrentReplicas      int32             `json:"currentReplicas"`
	ReadyReplicas        int32             `json:"readyReplicas"`
	DesiredReplicas      int32             `json:"desiredReplicas"`
	RecommendedReplicas  int32             `json:"recommendedReplicas"`
	EffectiveMinReplicas int32             `json:"effectiveMinReplicas"`
	EffectiveMaxReplicas int32             `json:"effectiveMaxReplicas"`
	Queue                *QueueStatus      `json:"queue,omitempty"`
	LastEvaluationTime   *metav1.MicroTime `json:"lastEvaluationTime,omitempty"`
	LastScaleTime        *metav1.MicroTime `json:"lastScaleTime,omitempty"`
	LastScaleAttemptTime *metav1.MicroTime `json:"lastScaleAttemptTime,omitempty"`
	LastScaleUpTime      *metav1.MicroTime `json:"lastScaleUpTime,omitempty"`
	LastScaleDownTime    *metav1.MicroTime `json:"lastScaleDownTime,omitempty"`
	IdleSince            *metav1.MicroTime `json:"idleSince,omitempty"`
	NextEvaluationTime   *metav1.MicroTime `json:"nextEvaluationTime,omitempty"`
	ActiveSchedule       string            `json:"activeSchedule,omitempty"`
	LastScalingReason    string            `json:"lastScalingReason,omitempty"`
	// +kubebuilder:validation:MaxItems=2048
	Recommendations []Recommendation `json:"recommendations,omitempty"`
	PendingScale    *PendingScale    `json:"pendingScale,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=wsp
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.scaleTargetRef.name`
// +kubebuilder:printcolumn:name="Current",type=integer,JSONPath=`.status.currentReplicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=`.status.desiredReplicas`
type WorkloadScalingPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              WorkloadScalingPolicySpec   `json:"spec"`
	Status            WorkloadScalingPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type WorkloadScalingPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkloadScalingPolicy `json:"items"`
}
