package scaling

import (
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	"testing"
)

func resources(cpu, memory string) corev1.ResourceList {
	return corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(memory)}
}
func TestSidecarInitAndOverhead(t *testing.T) {
	p := corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: resources("500m", "128Mi")}}}, InitContainers: []corev1.Container{{Name: "sidecar", RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways), Resources: corev1.ResourceRequirements{Requests: resources("100m", "32Mi")}}, {Name: "init", Resources: corev1.ResourceRequirements{Requests: resources("2", "64Mi")}}}, Overhead: resources("100m", "16Mi")}
	r := PodRequests(p)
	cpu := r[corev1.ResourceCPU]
	mem := r[corev1.ResourceMemory]
	if cpu.Cmp(resource.MustParse("2200m")) != 0 || mem.Cmp(resource.MustParse("176Mi")) != 0 {
		t.Fatal(r)
	}
	budget := resource.MustParse("4400m")
	maxR, e := ResourceCeiling(p, api.ResourcePolicy{MaxTotalCPU: &budget}, 20)
	if e != nil || maxR != 2 {
		t.Fatal(maxR, e)
	}
}
func TestBudgetExactAndMissingRequests(t *testing.T) {
	p := corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: resources("333m", "32Mi")}}}}
	cpu := resource.MustParse("1")
	reserved := resource.MustParse("1m")
	n, e := ResourceCeiling(p, api.ResourcePolicy{MaxTotalCPU: &cpu, ReservedCPU: &reserved}, 100)
	if e != nil || n != 3 {
		t.Fatal(n, e)
	}
	p.Containers = append(p.Containers, corev1.Container{Name: "missing"})
	_, e = ResourceCeiling(p, api.ResourcePolicy{MaxTotalCPU: &cpu}, 100)
	if e == nil {
		t.Fatal("missing requests must fail closed")
	}
}
