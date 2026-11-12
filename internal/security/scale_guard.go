// Package security enforces exclusive replica writes at the API admission boundary.
package security

import (
	"context"
	"encoding/json"
	"fmt"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ScaleGuard allows HPA creation (so conflicts are visible and recoverable), but
// rejects competing replica writes while an active policy exists. It also guards
// full Deployment updates that change replicas, including GitOps writers.
type ScaleGuard struct {
	Reader           client.Reader
	OperatorIdentity string
}

func (g *ScaleGuard) Handle(ctx context.Context, req admission.Request) admission.Response {
	var from, to int32
	if req.SubResource == "scale" {
		var old, next autoscalingv1.Scale
		if e := json.Unmarshal(req.OldObject.Raw, &old); e != nil {
			return admission.Errored(http.StatusBadRequest, e)
		}
		if e := json.Unmarshal(req.Object.Raw, &next); e != nil {
			return admission.Errored(http.StatusBadRequest, e)
		}
		from = old.Spec.Replicas
		to = next.Spec.Replicas
	} else {
		var old, next appsv1.Deployment
		if e := json.Unmarshal(req.OldObject.Raw, &old); e != nil {
			return admission.Errored(http.StatusBadRequest, e)
		}
		if e := json.Unmarshal(req.Object.Raw, &next); e != nil {
			return admission.Errored(http.StatusBadRequest, e)
		}
		if old.Spec.Replicas == nil || next.Spec.Replicas == nil {
			return admission.Allowed("replicas absent")
		}
		from = *old.Spec.Replicas
		to = *next.Spec.Replicas
	}
	if from == to {
		return admission.Allowed("no replica change")
	}
	var list api.WorkloadScalingPolicyList
	if e := g.Reader.List(ctx, &list, client.InNamespace(req.Namespace)); e != nil {
		return admission.Errored(http.StatusServiceUnavailable, fmt.Errorf("ownership lookup failed"))
	}
	var owner *api.WorkloadScalingPolicy
	for i := range list.Items {
		p := &list.Items[i]
		if p.Spec.ScaleTargetRef.Name == req.Name && !p.Spec.Paused && !p.Spec.DryRun && p.DeletionTimestamp == nil {
			if owner != nil {
				return admission.Denied("duplicate ScalePilot policies; all replica writes suspended")
			}
			owner = p
		}
	}
	if owner == nil {
		return admission.Allowed("unmanaged target")
	}
	if req.UserInfo.Username != g.OperatorIdentity {
		return admission.Denied("ScalePilot exclusively owns replicas; pause/delete its policy before changing replicas")
	}
	intent := owner.Status.PendingScale
	if intent == nil || intent.From != from || intent.To != to || owner.Status.ObservedGeneration != owner.Generation {
		return admission.Denied("replica change has no matching durable ScalePilot intent")
	}
	var hpas autoscalingv2.HorizontalPodAutoscalerList
	if e := g.Reader.List(ctx, &hpas, client.InNamespace(req.Namespace)); e != nil {
		return admission.Errored(http.StatusServiceUnavailable, fmt.Errorf("HPA lookup failed"))
	}
	for _, h := range hpas.Items {
		if h.Spec.ScaleTargetRef.Kind == "Deployment" && h.Spec.ScaleTargetRef.Name == req.Name {
			return admission.Denied("HPA conflict; ScalePilot replica writes suspended")
		}
	}
	return admission.Allowed("exclusive ScalePilot decision")
}
