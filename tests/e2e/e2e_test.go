//go:build e2e

// These tests mutate only the demo on the explicitly named Kind context.
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func kube(t *testing.T, input []byte, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	contextName := os.Getenv("SCALEPILOT_E2E_CONTEXT")
	if contextName == "" {
		contextName = "kind-scalepilot"
	}
	all := append([]string{"--context", contextName}, args...)
	cmd := exec.CommandContext(ctx, "kubectl", all...)
	if input != nil {
		cmd.Stdin = strings.NewReader(string(input))
	}
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("kubectl %v: %v\n%s", args, e, out)
	}
	return out
}
func patch(t *testing.T, resource, name string, value any) {
	t.Helper()
	b, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	kube(t, nil, "-n", "demo", "patch", resource, name, "--type=merge", "-p", string(b))
}
func policy(t *testing.T) api.WorkloadScalingPolicy {
	t.Helper()
	var p api.WorkloadScalingPolicy
	if e := json.Unmarshal(kube(t, nil, "-n", "demo", "get", "wsp", "document-worker-policy", "-o", "json"), &p); e != nil {
		t.Fatal(e)
	}
	return p
}
func deployment(t *testing.T) appsv1.Deployment {
	t.Helper()
	var d appsv1.Deployment
	if e := json.Unmarshal(kube(t, nil, "-n", "demo", "get", "deployment", "document-worker", "-o", "json"), &d); e != nil {
		t.Fatal(e)
	}
	return d
}
func eventually(t *testing.T, timeout time.Duration, description string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("timed out: " + description)
}
func diagnostics(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		dir := filepath.Join("..", "..", "artifacts", t.Name())
		_ = os.MkdirAll(dir, 0755)
		for name, args := range map[string][]string{"pods": {"get", "pods", "-A", "-o", "wide"}, "events": {"get", "events", "-A", "--sort-by=.lastTimestamp"}, "policy": {"-n", "demo", "get", "wsp", "-o", "yaml"}, "controller": {"-n", "scalepilot-system", "logs", "-l", "app.kubernetes.io/name=scalepilot", "--all-containers", "--tail=200"}} {
			cmd := exec.Command("kubectl", args...)
			out, _ := cmd.CombinedOutput()
			_ = os.WriteFile(filepath.Join(dir, name+".txt"), out, 0644)
		}
	})
}
func reset(t *testing.T) {
	t.Helper()
	diagnostics(t)
	patch(t, "wsp", "document-worker-policy", map[string]any{"spec": map[string]any{"paused": false, "dryRun": false, "minReplicas": 1, "maxReplicas": 6, "resourcePolicy": map[string]any{"maxTotalCPU": "600m", "maxTotalMemory": "1Gi"}, "behavior": map[string]any{"scaleUp": map[string]any{"cooldown": "10s", "maxPodsPerStep": 5, "maxPercentPerStep": 100}, "scaleDown": map[string]any{"cooldown": "10s", "maxPodsPerStep": 2, "maxPercentPerStep": 50}, "stabilizationWindow": "15s"}}})
	eventually(t, 180*time.Second, "demo idle and one ready worker", func() bool {
		p := policy(t)
		d := deployment(t)
		return p.Status.Queue != nil && p.Status.Queue.Pending == 0 && p.Status.Queue.InFlight == 0 && *d.Spec.Replicas == 1 && d.Status.ReadyReplicas == 1 && p.Status.ObservedGeneration == p.Generation
	})
}
func load(t *testing.T, jobs int, duration string) {
	t.Helper()
	kube(t, nil, "-n", "demo", "delete", "job", "e2e-load", "--ignore-not-found", "--wait=true")
	j := batchv1.Job{TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"}, ObjectMeta: metav1.ObjectMeta{Name: "e2e-load", Namespace: "demo"}, Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, Containers: []corev1.Container{{Name: "loadgen", Image: "scalepilot/loadgen:dev", ImagePullPolicy: corev1.PullIfNotPresent, Args: []string{fmt.Sprintf("--jobs=%d", jobs), "--rate=1000", "--pattern=spike", "--burst=1", "--duration=30s", "--job-duration=" + duration}, Env: []corev1.EnvVar{{Name: "REDIS_ADDRESS", Value: "redis.demo.svc.cluster.local:6379"}}}}}}}}
	b, _ := json.Marshal(j)
	kube(t, b, "apply", "-f", "-")
}
func TestScalingAndCooldown(t *testing.T) {
	reset(t)
	load(t, 1000, "300ms")
	eventually(t, 90*time.Second, "real queue backlog and scale-up", func() bool {
		p := policy(t)
		return p.Status.Queue != nil && p.Status.Queue.Pending > 0 && p.Status.DesiredReplicas > 1 && deployment(t).Status.ReadyReplicas > 1
	})
	p := policy(t)
	if p.Status.LastScaleTime == nil {
		t.Fatal("scale timestamp missing")
	}
	if *deployment(t).Spec.Replicas > 6 {
		t.Fatal("maximum exceeded")
	}
	first := p.Status.LastScaleTime.Time
	replicas := *deployment(t).Spec.Replicas
	if time.Since(first) < 8*time.Second {
		time.Sleep(2 * time.Second)
		if *deployment(t).Spec.Replicas != replicas {
			t.Fatal("replicas changed inside cooldown")
		}
	}
	eventually(t, 240*time.Second, "queue drains and gradual downscale returns to minimum", func() bool {
		p := policy(t)
		return p.Status.Queue != nil && p.Status.Queue.Pending == 0 && p.Status.Queue.InFlight == 0 && *deployment(t).Spec.Replicas == 1
	})
}
func TestConflict(t *testing.T) {
	reset(t)
	hpa := []byte(`{"apiVersion":"autoscaling/v2","kind":"HorizontalPodAutoscaler","metadata":{"name":"e2e-conflict","namespace":"demo"},"spec":{"scaleTargetRef":{"apiVersion":"apps/v1","kind":"Deployment","name":"document-worker"},"minReplicas":1,"maxReplicas":6,"metrics":[{"type":"Resource","resource":{"name":"cpu","target":{"type":"Utilization","averageUtilization":60}}}]}}`)
	kube(t, hpa, "apply", "-f", "-")
	t.Cleanup(func() { kube(t, nil, "-n", "demo", "delete", "hpa", "e2e-conflict", "--ignore-not-found") })
	eventually(t, 30*time.Second, "HPA conflict condition", func() bool { return meta.IsStatusConditionTrue(policy(t).Status.Conditions, "ConflictDetected") })
	events := kube(t, nil, "-n", "demo", "get", "events", "--field-selector=reason=ScalingConflictDetected", "-o", "json")
	if !strings.Contains(string(events), "e2e-conflict") {
		t.Fatal("conflict event missing")
	}
	p := policy(t)
	if meta.IsStatusConditionTrue(p.Status.Conditions, "ScalingActive") {
		t.Fatal("scaling stayed active")
	}
	kube(t, nil, "-n", "demo", "delete", "hpa", "e2e-conflict")
	eventually(t, 30*time.Second, "ownership resumes", func() bool { return !meta.IsStatusConditionTrue(policy(t).Status.Conditions, "ConflictDetected") })
}
func TestRedisOutage(t *testing.T) {
	reset(t)
	before := *deployment(t).Spec.Replicas
	kube(t, nil, "-n", "demo", "scale", "deployment/redis", "--replicas=0")
	t.Cleanup(func() { kube(t, nil, "-n", "demo", "scale", "deployment/redis", "--replicas=1") })
	eventually(t, 30*time.Second, "metrics outage status", func() bool { return meta.IsStatusConditionFalse(policy(t).Status.Conditions, "MetricsAvailable") })
	time.Sleep(12 * time.Second)
	if *deployment(t).Spec.Replicas != before {
		t.Fatal("Redis outage changed worker replicas")
	}
	kube(t, nil, "-n", "demo", "scale", "deployment/redis", "--replicas=1")
	eventually(t, 90*time.Second, "Redis metrics recover", func() bool { return meta.IsStatusConditionTrue(policy(t).Status.Conditions, "MetricsAvailable") })
}
func TestFailover(t *testing.T) {
	reset(t)
	var lease coordinationv1.Lease
	if e := json.Unmarshal(kube(t, nil, "-n", "scalepilot-system", "get", "lease", "scalepilot-manager.scalepilot.io", "-o", "json"), &lease); e != nil {
		t.Fatal(e)
	}
	if lease.Spec.HolderIdentity == nil {
		t.Fatal("no leader")
	}
	old := *lease.Spec.HolderIdentity
	leader := strings.Split(old, "_")[0]
	before := policy(t)
	kube(t, nil, "-n", "scalepilot-system", "delete", "pod", leader, "--grace-period=0", "--force")
	eventually(t, 90*time.Second, "new leader resumes reconciliation", func() bool {
		var l coordinationv1.Lease
		_ = json.Unmarshal(kube(t, nil, "-n", "scalepilot-system", "get", "lease", "scalepilot-manager.scalepilot.io", "-o", "json"), &l)
		p := policy(t)
		return l.Spec.HolderIdentity != nil && *l.Spec.HolderIdentity != old && p.Status.LastEvaluationTime != nil && p.Status.LastEvaluationTime.After(before.Status.LastEvaluationTime.Time)
	})
	after := policy(t)
	if before.Status.LastScaleTime != nil && (after.Status.LastScaleTime == nil || after.Status.LastScaleTime.Before(before.Status.LastScaleTime)) {
		t.Fatal("failover lost durable scale timestamp")
	}
}
func TestWorkerCrashRecovery(t *testing.T) {
	reset(t)
	load(t, 10, "5s")
	eventually(t, 60*time.Second, "in-flight jobs before crash", func() bool { p := policy(t); return p.Status.Queue != nil && p.Status.Queue.InFlight > 0 })
	pods := kube(t, nil, "-n", "demo", "get", "pods", "-l", "app=document-worker", "-o", "jsonpath={.items[0].metadata.name}")
	kube(t, nil, "-n", "demo", "delete", "pod", string(pods), "--grace-period=0", "--force")
	eventually(t, 180*time.Second, "PEL recovered and all jobs acknowledged", func() bool {
		p := policy(t)
		return p.Status.Queue != nil && p.Status.Queue.Pending == 0 && p.Status.Queue.InFlight == 0 && p.Status.Queue.ExecutionSamples > 0
	})
}
func TestResourceBudgetAndUnavailableCapacity(t *testing.T) {
	reset(t)
	patch(t, "wsp", "document-worker-policy", map[string]any{"spec": map[string]any{"resourcePolicy": map[string]any{"maxTotalCPU": "200m"}}})
	load(t, 400, "100ms")
	eventually(t, 60*time.Second, "budget caps demand at two replicas", func() bool {
		p := policy(t)
		return p.Status.EffectiveMaxReplicas == 2 && p.Status.DesiredReplicas == 2
	})
	if *deployment(t).Spec.Replicas > 2 {
		t.Fatal("resource budget exceeded")
	}
	eventually(t, 120*time.Second, "work drains", func() bool {
		p := policy(t)
		return p.Status.Queue != nil && p.Status.Queue.Pending == 0 && p.Status.Queue.InFlight == 0
	})
	strategy := deployment(t).Spec.Strategy
	patch(t, "deployment", "document-worker", map[string]any{"spec": map[string]any{"strategy": map[string]any{"type": "RollingUpdate", "rollingUpdate": map[string]any{"maxUnavailable": 1, "maxSurge": 0}}, "template": map[string]any{"spec": map[string]any{"nodeSelector": map[string]string{"scalepilot.io/nonexistent": "true"}}}}})
	t.Cleanup(func() {
		patch(t, "deployment", "document-worker", map[string]any{"spec": map[string]any{"strategy": strategy, "template": map[string]any{"spec": map[string]any{"nodeSelector": nil}}}})
	})
	eventually(t, 60*time.Second, "unavailable capacity reported accurately", func() bool {
		p := policy(t)
		return p.Status.ReadyReplicas < p.Status.CurrentReplicas && meta.IsStatusConditionTrue(p.Status.Conditions, "Degraded")
	})
}

func TestAdmissionRejectsCompetingReplicaWrites(t *testing.T) {
	reset(t)
	before := *deployment(t).Spec.Replicas
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	contextName := os.Getenv("SCALEPILOT_E2E_CONTEXT")
	if contextName == "" {
		contextName = "kind-scalepilot"
	}
	cmd := exec.CommandContext(ctx, "kubectl", "--context", contextName, "-n", "demo", "scale", "deployment/document-worker", "--replicas=6")
	out, e := cmd.CombinedOutput()
	if e == nil || !strings.Contains(string(out), "exclusively owns replicas") {
		t.Fatalf("competing writer was not rejected: %v %s", e, out)
	}
	if *deployment(t).Spec.Replicas != before {
		t.Fatal("admission allowed competing write")
	}
}
