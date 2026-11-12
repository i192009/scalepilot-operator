package workload

import (
	"context"
	"fmt"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Scaler interface {
	Read(context.Context, *appsv1.Deployment) (*autoscalingv1.Scale, error)
	Set(context.Context, *api.WorkloadScalingPolicy, *appsv1.Deployment, int32, int32) error
}
type Executor struct {
	Client client.Client
	Reader client.Reader
	Guard  Guard
}

func (e Executor) Read(ctx context.Context, d *appsv1.Deployment) (*autoscalingv1.Scale, error) {
	s := &autoscalingv1.Scale{}
	err := e.Client.SubResource("scale").Get(ctx, d, s)
	return s, err
}
func (e Executor) Set(ctx context.Context, p *api.WorkloadScalingPolicy, d *appsv1.Deployment, from, to int32) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var livePolicy api.WorkloadScalingPolicy
		if err := e.Reader.Get(ctx, client.ObjectKeyFromObject(p), &livePolicy); err != nil {
			return err
		}
		if livePolicy.UID != p.UID || livePolicy.Generation != p.Generation || livePolicy.DeletionTimestamp != nil || livePolicy.Spec.Paused || livePolicy.Spec.DryRun {
			return fmt.Errorf("policy changed during decision")
		}
		var live appsv1.Deployment
		if err := e.Reader.Get(ctx, client.ObjectKeyFromObject(d), &live); err != nil {
			return err
		}
		if live.UID != d.UID {
			return fmt.Errorf("target replaced during decision")
		}
		if err := e.Guard.Check(ctx, &livePolicy); err != nil {
			return err
		}
		if err := e.Guard.Acquire(ctx, &livePolicy, &live); err != nil {
			return err
		}
		s, err := e.Read(ctx, &live)
		if err != nil {
			return err
		}
		if s.Spec.Replicas != from {
			return fmt.Errorf("replicas changed concurrently; reevaluation required")
		}
		s.Spec.Replicas = to
		err = e.Client.SubResource("scale").Update(ctx, &live, client.WithSubResourceBody(s))
		if apierrors.IsConflict(err) {
			return apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments/scale"}, live.Name, err)
		}
		return err
	})
}
