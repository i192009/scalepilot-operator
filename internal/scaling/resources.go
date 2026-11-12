package scaling

import (
	"fmt"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"math/big"
)

// PodRequests implements scheduling semantics for regular and restartable init
// containers: max(sum(app)+sum(sidecars), each init+preceding sidecars)+overhead.
func PodRequests(p corev1.PodSpec) corev1.ResourceList {
	app := corev1.ResourceList{}
	sidecars := corev1.ResourceList{}
	peak := corev1.ResourceList{}
	for _, c := range p.Containers {
		add(app, c.Resources.Requests)
	}
	for _, c := range p.InitContainers {
		stage := corev1.ResourceList{}
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			add(sidecars, c.Resources.Requests)
			add(stage, sidecars)
		} else {
			add(stage, sidecars)
			add(stage, c.Resources.Requests)
		}
		maximum(peak, stage)
	}
	add(app, sidecars)
	maximum(app, peak)
	// Pod-level requests, when set, replace container sums for the named resources.
	if p.Resources != nil {
		for k, v := range p.Resources.Requests {
			app[k] = v.DeepCopy()
		}
	}
	add(app, p.Overhead)
	return app
}
func add(dst, src corev1.ResourceList) {
	for k, v := range src {
		q := dst[k]
		q.Add(v)
		dst[k] = q
	}
}
func maximum(dst, src corev1.ResourceList) {
	for k, v := range src {
		q, ok := dst[k]
		if !ok || v.Cmp(q) > 0 {
			dst[k] = v.DeepCopy()
		}
	}
}

// ResourceCeiling uses exact rational quantities, with no float rounding.
// Missing/zero requests under an enabled budget fail closed.
func ResourceCeiling(p corev1.PodSpec, r api.ResourcePolicy, ceiling int32) (int32, error) {
	if r.ReplicaCeiling != nil {
		ceiling = min(ceiling, *r.ReplicaCeiling)
	}
	requests := PodRequests(p)
	for _, v := range []struct {
		key              corev1.ResourceName
		budget, reserved *resource.Quantity
	}{{corev1.ResourceCPU, r.MaxTotalCPU, r.ReservedCPU}, {corev1.ResourceMemory, r.MaxTotalMemory, r.ReservedMemory}} {
		if v.budget == nil {
			continue
		}
		request := requests[v.key]
		if request.Sign() <= 0 {
			return 0, fmt.Errorf("%s requests required for all budgeted Pods", v.key)
		}
		// Reject missing requests on any app/init container. Summing one declared request is insufficient.
		podRequest := resource.Quantity{}
		if p.Resources != nil {
			podRequest = p.Resources.Requests[v.key]
		}
		if podRequest.IsZero() {
			for _, c := range append(append([]corev1.Container{}, p.Containers...), p.InitContainers...) {
				q := c.Resources.Requests[v.key]
				if q.Sign() <= 0 {
					return 0, fmt.Errorf("%s request missing on container %s", v.key, c.Name)
				}
			}
		}
		budget := v.budget.DeepCopy()
		if v.reserved != nil {
			budget.Sub(*v.reserved)
		}
		if budget.Sign() <= 0 {
			return 0, nil
		}
		a, _ := new(big.Rat).SetString(budget.AsDec().String())
		b, _ := new(big.Rat).SetString(request.AsDec().String())
		a.Quo(a, b)
		n := new(big.Int).Quo(a.Num(), a.Denom())
		if n.Cmp(big.NewInt(int64(ceiling))) < 0 {
			ceiling = int32(n.Int64())
		}
	}
	return max(0, ceiling), nil
}
