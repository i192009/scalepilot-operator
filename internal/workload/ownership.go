package workload

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var ErrOwnership = errors.New("scaling ownership conflict")

// Guard always uses an uncached reader; an informer cache is not sufficient for
// exclusive ownership decisions. Lease creation/CAS serializes competing policies.
type Guard struct {
	Reader client.Reader
	Client client.Client
}

func (g Guard) Check(ctx context.Context, p *api.WorkloadScalingPolicy) error {
	var hpas autoscalingv2.HorizontalPodAutoscalerList
	if e := g.Reader.List(ctx, &hpas, client.InNamespace(p.Namespace)); e != nil {
		return e
	}
	for _, h := range hpas.Items {
		r := h.Spec.ScaleTargetRef
		if r.Name == p.Spec.ScaleTargetRef.Name && r.Kind == "Deployment" && (r.APIVersion == "apps/v1" || r.APIVersion == "apps/v1beta1" || r.APIVersion == "apps/v1beta2") {
			return fmt.Errorf("%w: HPA %s targets this Deployment", ErrOwnership, h.Name)
		}
	}
	var policies api.WorkloadScalingPolicyList
	if e := g.Reader.List(ctx, &policies, client.InNamespace(p.Namespace)); e != nil {
		return e
	}
	for _, other := range policies.Items {
		if other.UID != p.UID && other.DeletionTimestamp == nil && !other.Spec.Paused && !other.Spec.DryRun && other.Spec.ScaleTargetRef.Name == p.Spec.ScaleTargetRef.Name {
			return fmt.Errorf("%w: policy %s targets this Deployment", ErrOwnership, other.Name)
		}
	}
	return nil
}
func (g Guard) Acquire(ctx context.Context, p *api.WorkloadScalingPolicy, d *appsv1.Deployment) error {
	sum := sha256.Sum256([]byte(d.UID))
	key := types.NamespacedName{Namespace: p.Namespace, Name: fmt.Sprintf("scalepilot-%x", sum[:16])}
	var lease coordinationv1.Lease
	err := g.Reader.Get(ctx, key, &lease)
	if apierrors.IsNotFound(err) {
		lease = coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(p, api.GroupVersion.WithKind("WorkloadScalingPolicy"))}}, Spec: coordinationv1.LeaseSpec{HolderIdentity: ptr.To(string(p.UID))}}
		if e := g.Client.Create(ctx, &lease); e != nil {
			if apierrors.IsAlreadyExists(e) {
				return fmt.Errorf("%w: ownership acquisition raced", ErrOwnership)
			}
			return e
		}
		return nil
	}
	if err != nil {
		return err
	}
	if lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity == string(p.UID) {
		return nil
	}
	// No wall-clock expiry: a stalled policy must not lose its fence while its
	// controller may still be executing. Only an absent or explicitly inactive owner permits takeover.
	var owner api.WorkloadScalingPolicy
	if len(lease.OwnerReferences) != 1 {
		return fmt.Errorf("%w: unrecognized ownership Lease", ErrOwnership)
	}
	ref := lease.OwnerReferences[0]
	e := g.Reader.Get(ctx, types.NamespacedName{Namespace: p.Namespace, Name: ref.Name}, &owner)
	if e != nil && !apierrors.IsNotFound(e) {
		return e
	}
	if e == nil && owner.UID == ref.UID && owner.DeletionTimestamp == nil && !owner.Spec.Paused && !owner.Spec.DryRun {
		return fmt.Errorf("%w: another active policy owns target", ErrOwnership)
	}
	lease.Spec.HolderIdentity = ptr.To(string(p.UID))
	lease.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(p, api.GroupVersion.WithKind("WorkloadScalingPolicy"))}
	if e = g.Client.Update(ctx, &lease); apierrors.IsConflict(e) {
		return fmt.Errorf("%w: Lease CAS conflict", ErrOwnership)
	}
	return e
}
