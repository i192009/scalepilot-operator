package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	collector "github.com/scalepilot/scalepilot-operator/internal/metrics"
	obs "github.com/scalepilot/scalepilot-operator/internal/observability"
	"github.com/scalepilot/scalepilot-operator/internal/policy"
	"github.com/scalepilot/scalepilot-operator/internal/scaling"
	"github.com/scalepilot/scalepilot-operator/internal/workload"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const TargetIndex = "spec.scaleTargetRef.name"

type EventRecorder interface {
	Event(runtime.Object, string, string, string)
}

type WorkloadScalingPolicyReconciler struct {
	client.Client
	Reader          client.Reader
	Scheme          *runtime.Scheme
	Provider        collector.Provider
	Scaler          workload.Scaler
	Guard           workload.Guard
	Recorder        EventRecorder
	Now             func() time.Time
	DefaultInterval time.Duration
}

// +kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=workloadscalingpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=workloadscalingpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments/scale,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch
func (r *WorkloadScalingPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, span := otel.Tracer("scalepilot/controller").Start(ctx, "reconcile")
	defer span.End()
	span.SetAttributes(attribute.String("policy.namespace", req.Namespace), attribute.String("policy.name", req.Name))
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var p api.WorkloadScalingPolicy
	if e := r.Get(ctx, req.NamespacedName, &p); e != nil {
		if apierrors.IsNotFound(e) {
			obs.Delete(req.Namespace, req.Name)
		}
		return ctrl.Result{}, client.IgnoreNotFound(e)
	}
	if !p.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	interval := p.Spec.Metrics.Redis.CollectionInterval.Duration
	if interval < time.Second {
		interval = r.DefaultInterval
		if interval < time.Second {
			interval = 5 * time.Second
		}
	}
	result := ctrl.Result{RequeueAfter: interval}
	before := p.DeepCopy()
	if err := policy.Validate(p.Spec); err != nil {
		r.condition(&p, now, "Ready", false, "InvalidPolicy", err.Error())
		r.condition(&p, now, "Degraded", true, "InvalidPolicy", err.Error())
		r.condition(&p, now, "ScalingActive", false, "InvalidPolicy", "invalid configuration; no replica writes")
		p.Status.ObservedGeneration = p.Generation
		r.eventChange(before, &p, "InvalidPolicy", err.Error())
		return result, r.save(ctx, before, &p)
	}
	var d appsv1.Deployment
	if e := r.Reader.Get(ctx, types.NamespacedName{Namespace: p.Namespace, Name: p.Spec.ScaleTargetRef.Name}, &d); e != nil {
		r.condition(&p, now, "Ready", false, "TargetUnavailable", "target Deployment unavailable")
		r.condition(&p, now, "Degraded", true, "TargetUnavailable", "target Deployment unavailable")
		r.condition(&p, now, "ScalingActive", false, "TargetUnavailable", "no replica writes")
		p.Status.ObservedGeneration = p.Generation
		if err := r.save(ctx, before, &p); err != nil {
			return result, err
		}
		return result, client.IgnoreNotFound(e)
	}
	scale, e := r.Scaler.Read(ctx, &d)
	if e != nil {
		return result, e
	}
	current := scale.Spec.Replicas
	p.Status.CurrentReplicas = current
	p.Status.ReadyReplicas = d.Status.ReadyReplicas
	p.Status.ObservedGeneration = p.Generation
	if !p.Spec.Paused && !p.Spec.DryRun {
		e = r.Guard.Check(ctx, &p)
		if e == nil {
			e = r.Guard.Acquire(ctx, &p, &d)
		}
		if e != nil {
			if !errors.Is(e, workload.ErrOwnership) {
				return result, e
			}
			p.Status.DesiredReplicas = current
			p.Status.LastScalingReason = "ScalingConflictDetected"
			r.condition(&p, now, "ConflictDetected", true, "CompetingAutoscaler", e.Error())
			r.condition(&p, now, "ScalingActive", false, "CompetingAutoscaler", "replica writes suspended")
			r.condition(&p, now, "Ready", false, "CompetingAutoscaler", e.Error())
			r.condition(&p, now, "Degraded", true, "CompetingAutoscaler", e.Error())
			r.eventChange(before, &p, "ScalingConflictDetected", e.Error())
			return result, r.save(ctx, before, &p)
		}
	}
	wasConflict := meta.IsStatusConditionTrue(before.Status.Conditions, "ConflictDetected")
	if p.Status.NextEvaluationTime != nil && now.Before(p.Status.NextEvaluationTime.Time) && p.Status.ObservedGeneration == before.Status.ObservedGeneration && !wasConflict && p.Status.PendingScale == nil {
		// Workload readiness changes are meaningful even between metric samples.
		if d.Status.ReadyReplicas < current {
			r.condition(&p, now, "Ready", false, "CapacityNotReady", "requested replicas are not all ready")
			r.condition(&p, now, "Degraded", true, "CapacityNotReady", "Kubernetes scheduling and startup may be limiting capacity")
		}
		result.RequeueAfter = p.Status.NextEvaluationTime.Sub(now)
		return result, r.save(ctx, before, &p)
	}
	r.condition(&p, now, "ConflictDetected", false, "ExclusiveOwnership", "no competing autoscaler observed")
	if wasConflict {
		r.event(&p, corev1.EventTypeNormal, "ScalingResumed", "exclusive ownership restored")
	}
	// Recover a durable intent conservatively. Its timestamp fences even an
	// ambiguous write. The current scale value is authoritative, not our old desired value.
	if pending := p.Status.PendingScale; pending != nil {
		p.Status.LastScaleAttemptTime = &pending.Time
		// A failed/ambiguous attempt consumes cooldown but is not reported as a
		// successful scale. Recover success only when this target UID is at To.
		if pending.TargetUID == string(d.UID) && current == pending.To {
			p.Status.LastScaleTime = &pending.Time
			if pending.To > pending.From {
				p.Status.LastScaleUpTime = &pending.Time
			} else {
				p.Status.LastScaleDownTime = &pending.Time
			}
		}
		p.Status.PendingScale = nil
	}
	ceiling, e := scaling.ResourceCeiling(d.Spec.Template.Spec, p.Spec.ResourcePolicy, p.Spec.MaxReplicas)
	if e != nil {
		r.condition(&p, now, "Ready", false, "InvalidResourceRequests", e.Error())
		r.condition(&p, now, "Degraded", true, "InvalidResourceRequests", e.Error())
		r.condition(&p, now, "ScalingActive", false, "InvalidResourceRequests", "budget cannot be evaluated")
		return result, r.save(ctx, before, &p)
	}
	started := time.Now()
	sample, collectErr := r.Provider.Collect(ctx, &p)
	obs.Collection.Observe(time.Since(started).Seconds())
	if collectErr == nil {
		collectErr = scaling.ValidSample(sample, now, p.Spec.Metrics.MaxSampleAge.Duration)
	}
	if collectErr != nil {
		sample = nil
		obs.Errors.WithLabelValues("metrics").Inc()
		r.condition(&p, now, "MetricsAvailable", false, "CollectionFailed", "metrics unavailable or stale; downscaling suspended")
		r.eventChange(before, &p, "MetricsUnavailable", "Redis metrics unavailable or stale")
	} else {
		p.Status.Queue = sample
		r.condition(&p, now, "MetricsAvailable", true, "FreshSample", "Redis queue snapshot collected")
	}
	decision, e := scaling.Evaluate(scaling.Input{Policy: p.Spec, Status: p.Status, Metrics: sample, Current: current, Ready: d.Status.ReadyReplicas, ResourceCeiling: ceiling, Now: now})
	if e != nil {
		return result, e
	}
	p.Status.DesiredReplicas = decision.Desired
	p.Status.RecommendedReplicas = decision.Recommended
	p.Status.EffectiveMinReplicas = decision.Minimum
	p.Status.EffectiveMaxReplicas = decision.Maximum
	p.Status.ActiveSchedule = decision.Schedule
	p.Status.LastScalingReason = decision.Reason
	p.Status.IdleSince = decision.IdleSince
	p.Status.Recommendations = decision.History
	evaluated := metav1.NewMicroTime(now)
	next := metav1.NewMicroTime(now.Add(interval))
	p.Status.LastEvaluationTime = &evaluated
	p.Status.NextEvaluationTime = &next
	active := !p.Spec.Paused && !p.Spec.DryRun && collectErr == nil
	r.condition(&p, now, "ScalingActive", active, decision.Reason, "scaling evaluation completed")
	r.condition(&p, now, "ScalingLimited", decision.Limited, decision.Reason, fmt.Sprintf("effective bounds %d..%d; recommended %d; allowed %d", decision.Minimum, decision.Maximum, decision.Recommended, decision.Desired))
	degraded := collectErr != nil || d.Status.ReadyReplicas < current || ceiling < p.Spec.MinReplicas
	r.condition(&p, now, "Degraded", degraded, "CapacityAndMetrics", fmt.Sprintf("requested=%d ready=%d resource ceiling=%d; metrics available=%t", current, d.Status.ReadyReplicas, ceiling, collectErr == nil))
	r.condition(&p, now, "Ready", !degraded, "EvaluationComplete", "policy evaluated; inspect capacity and metrics conditions")
	if decision.Limited {
		obs.Blocked.WithLabelValues(decision.Reason).Inc()
	}
	if decision.Schedule != before.Status.ActiveSchedule {
		r.event(&p, corev1.EventTypeNormal, "ScheduleActivated", "active schedule: "+decision.Schedule)
	}
	if decision.Desired != current && !p.Spec.Paused && !p.Spec.DryRun {
		// Commit intent with resourceVersion precondition before touching replicas.
		p.Status.PendingScale = &api.PendingScale{Time: evaluated, From: current, To: decision.Desired, TargetUID: string(d.UID)}
		p.Status.LastScaleAttemptTime = &evaluated
		if e = r.save(ctx, before, &p); e != nil {
			return result, e
		}
		before = p.DeepCopy()
		started = time.Now()
		e = r.Scaler.Set(ctx, &p, &d, current, decision.Desired)
		obs.APIDuration.Observe(time.Since(started).Seconds())
		if e != nil {
			obs.Errors.WithLabelValues("scale").Inc()
			r.event(&p, corev1.EventTypeWarning, "ScaleOperationFailed", "replica write failed; persisted intent retained for safe retry")
			return result, e
		}
		p.Status.LastScaleTime = &evaluated
		p.Status.PendingScale = nil
		direction := "down"
		reason := "ScaleDownTriggered"
		if decision.Desired > current {
			direction = "up"
			reason = "ScaleUpTriggered"
			p.Status.LastScaleUpTime = &evaluated
		} else {
			p.Status.LastScaleDownTime = &evaluated
		}
		obs.Operations.WithLabelValues(p.Namespace, p.Name, direction).Inc()
		r.event(&p, corev1.EventTypeNormal, reason, fmt.Sprintf("replicas %d -> %d: %s", current, decision.Desired, decision.Reason))
	}
	ctrl.LoggerFrom(ctx).Info("scaling evaluated", "currentReplicas", current, "readyReplicas", d.Status.ReadyReplicas, "recommendedReplicas", decision.Recommended, "finalReplicas", decision.Desired, "reason", decision.Reason)
	return result, r.save(ctx, before, &p)
}
func (r *WorkloadScalingPolicyReconciler) save(ctx context.Context, before, p *api.WorkloadScalingPolicy) error {
	obs.Publish(p)
	if equality.Semantic.DeepEqual(before.Status, p.Status) {
		return nil
	}
	return r.Status().Update(ctx, p)
}
func (r *WorkloadScalingPolicyReconciler) condition(p *api.WorkloadScalingPolicy, now time.Time, kind string, value bool, reason, message string) {
	s := metav1.ConditionFalse
	if value {
		s = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{Type: kind, Status: s, Reason: reason, Message: message, ObservedGeneration: p.Generation, LastTransitionTime: metav1.NewTime(now)})
}
func (r *WorkloadScalingPolicyReconciler) event(p *api.WorkloadScalingPolicy, kind, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(p, kind, reason, message)
	}
}
func (r *WorkloadScalingPolicyReconciler) eventChange(before, p *api.WorkloadScalingPolicy, reason, message string) {
	for _, c := range before.Status.Conditions {
		if c.Reason == reason && c.Message == message {
			return
		}
	}
	if before.Status.LastScalingReason == reason {
		return
	}
	r.event(p, corev1.EventTypeWarning, reason, message)
}
func (r *WorkloadScalingPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if e := mgr.GetFieldIndexer().IndexField(context.Background(), &api.WorkloadScalingPolicy{}, TargetIndex, func(o client.Object) []string {
		return []string{o.(*api.WorkloadScalingPolicy).Spec.ScaleTargetRef.Name}
	}); e != nil {
		return e
	}
	enqueue := func(ctx context.Context, o client.Object) []reconcile.Request {
		target := o.GetName()
		switch v := o.(type) {
		case *autoscalingv2.HorizontalPodAutoscaler:
			target = v.Spec.ScaleTargetRef.Name
		}
		var list api.WorkloadScalingPolicyList
		if e := r.List(ctx, &list, client.InNamespace(o.GetNamespace()), client.MatchingFields{TargetIndex: target}); e != nil {
			return nil
		}
		out := make([]reconcile.Request, 0, len(list.Items))
		for _, p := range list.Items {
			out = append(out, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&p)})
		}
		return out
	}
	secretEnqueue := func(ctx context.Context, o client.Object) []reconcile.Request {
		var list api.WorkloadScalingPolicyList
		if e := r.List(ctx, &list, client.InNamespace(o.GetNamespace())); e != nil {
			return nil
		}
		var out []reconcile.Request
		for _, p := range list.Items {
			if p.Spec.Metrics.Redis.SecretRef.Name == o.GetName() {
				out = append(out, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&p)})
			}
		}
		return out
	}
	return ctrl.NewControllerManagedBy(mgr).For(&api.WorkloadScalingPolicy{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(enqueue)).Watches(&autoscalingv2.HorizontalPodAutoscaler{}, handler.EnqueueRequestsFromMapFunc(enqueue)).WatchesMetadata(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(secretEnqueue)).Named("workloadscalingpolicy").Complete(r)
}
