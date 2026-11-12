//go:build envtest

package envtest_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"github.com/scalepilot/scalepilot-operator/internal/controller"
	"github.com/scalepilot/scalepilot-operator/internal/testutil"
	"github.com/scalepilot/scalepilot-operator/internal/workload"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var kube client.Client
var environment *envtest.Environment
var ctx = context.Background()

func TestMain(m *testing.M) {
	scheme := runtime.NewScheme()
	_ = api.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = autoscalingv2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	environment = &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases")}, ErrorIfCRDPathMissing: true}
	config, e := environment.Start()
	if e != nil {
		_ = environment.Stop()
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	// The workload guard also uses coordination Leases.
	_ = addLeaseScheme(scheme)
	kube, e = client.New(config, client.Options{Scheme: scheme})
	if e != nil {
		panic(e)
	}
	code := m.Run()
	if e = environment.Stop(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		code = 1
	}
	os.Exit(code)
}

type source struct {
	sample *api.QueueStatus
	err    error
}

func (s *source) Collect(context.Context, *api.WorkloadScalingPolicy) (*api.QueueStatus, error) {
	return s.sample, s.err
}
func setup(t *testing.T) (*api.WorkloadScalingPolicy, *source, *controller.WorkloadScalingPolicyReconciler, *time.Time) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "scalepilot-"}}
	must(t, kube.Create(ctx, ns))
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: ns.Name, Annotations: map[string]string{"preserve": "value"}}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To(int32(1)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "worker"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "worker"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "worker:dev"}}}}}}
	must(t, kube.Create(ctx, d))
	d.Status.Replicas = 1
	d.Status.ReadyReplicas = 1
	must(t, kube.Status().Update(ctx, d))
	p := &api.WorkloadScalingPolicy{ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: ns.Name}, Spec: testutil.Spec()}
	must(t, kube.Create(ctx, p))
	now := time.Now().UTC().Truncate(time.Second)
	s := &source{sample: &api.QueueStatus{Pending: 1000, CollectedAt: metav1.NewTime(now)}}
	guard := workload.Guard{Client: kube, Reader: kube}
	r := &controller.WorkloadScalingPolicyReconciler{Client: kube, Reader: kube, Provider: s, Guard: guard, Scaler: workload.Executor{Client: kube, Reader: kube, Guard: guard}, Recorder: record.NewFakeRecorder(100), Now: func() time.Time { return now }}
	return p, s, r, &now
}
func run(t *testing.T, r *controller.WorkloadScalingPolicyReconciler, p *api.WorkloadScalingPolicy) {
	t.Helper()
	_, e := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)})
	must(t, e)
	must(t, kube.Get(ctx, client.ObjectKeyFromObject(p), p))
}
func replicas(t *testing.T, p *api.WorkloadScalingPolicy) int32 {
	t.Helper()
	var d appsv1.Deployment
	must(t, kube.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "worker"}, &d))
	return *d.Spec.Replicas
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func TestScaleStatusAndOwnershipConflict(t *testing.T) {
	p, _, r, now := setup(t)
	run(t, r, p)
	if replicas(t, p) != 2 || p.Status.ObservedGeneration != p.Generation || p.Status.LastScaleTime == nil {
		t.Fatalf("scale/status not applied: %+v", p.Status)
	}
	h := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "competing", Namespace: p.Namespace}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "worker"}, MinReplicas: ptr.To(int32(1)), MaxReplicas: 20}}
	must(t, kube.Create(ctx, h))
	*now = now.Add(15 * time.Second)
	run(t, r, p)
	if replicas(t, p) != 2 || !meta.IsStatusConditionTrue(p.Status.Conditions, "ConflictDetected") {
		t.Fatal("HPA conflict not suspended")
	}
	recorder := r.Recorder.(*record.FakeRecorder)
	found := false
	for len(recorder.Events) > 0 {
		if contains(<-recorder.Events, "ScalingConflictDetected") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing conflict event")
	}
	must(t, kube.Delete(ctx, h))
	*now = now.Add(15 * time.Second)
	run(t, r, p)
	if meta.IsStatusConditionTrue(p.Status.Conditions, "ConflictDetected") || replicas(t, p) != 4 {
		t.Fatal("ownership did not resume")
	}
}
func TestDuplicatePoliciesFailClosed(t *testing.T) {
	p, _, r, _ := setup(t)
	other := p.DeepCopy()
	other.ResourceVersion = ""
	other.UID = ""
	other.Name = "duplicate"
	must(t, kube.Create(ctx, other))
	run(t, r, p)
	run(t, r, other)
	if replicas(t, p) != 1 || !meta.IsStatusConditionTrue(p.Status.Conditions, "ConflictDetected") || !meta.IsStatusConditionTrue(other.Status.Conditions, "ConflictDetected") {
		t.Fatal("duplicates allowed a writer")
	}
}
func TestRedisOutageAndRestartCooldown(t *testing.T) {
	p, s, r, now := setup(t)
	run(t, r, p)
	saved := p.Status.LastScaleTime.DeepCopy()
	*now = now.Add(6 * time.Second)
	s.err = errors.New("offline")
	run(t, r, p)
	if replicas(t, p) != 2 || meta.IsStatusConditionTrue(p.Status.Conditions, "MetricsAvailable") {
		t.Fatal("outage unsafe")
	}
	s.err = nil
	s.sample.CollectedAt = metav1.NewTime(*now)
	p.Status.NextEvaluationTime = nil
	must(t, kube.Status().Update(ctx, p))
	replacement := *r
	run(t, &replacement, p)
	if replicas(t, p) != 2 || !p.Status.LastScaleTime.Equal(saved) {
		t.Fatal("restart reset cooldown")
	}
}
func TestPendingIntentRecovery(t *testing.T) {
	p, _, r, now := setup(t)
	p.Status.PendingScale = &api.PendingScale{Time: metav1.NewMicroTime(*now), From: 1, To: 2, TargetUID: "previous"}
	must(t, kube.Status().Update(ctx, p))
	run(t, r, p)
	if replicas(t, p) != 1 || p.Status.PendingScale != nil || p.Status.LastScaleAttemptTime == nil || p.Status.LastScaleTime != nil {
		t.Fatal("pending intent was not conservatively fenced")
	}
}
func TestPauseDryRunDeletionAndNoStatusLoop(t *testing.T) {
	p, _, r, _ := setup(t)
	p.Spec.Paused = true
	must(t, kube.Update(ctx, p))
	run(t, r, p)
	if replicas(t, p) != 1 {
		t.Fatal("paused writer")
	}
	rv := p.ResourceVersion
	run(t, r, p)
	if p.ResourceVersion != rv {
		t.Fatal("unchanged reconcile wrote status")
	}
	p.Spec.Paused = false
	p.Spec.DryRun = true
	must(t, kube.Update(ctx, p))
	run(t, r, p)
	if replicas(t, p) != 1 || p.Status.RecommendedReplicas <= 1 {
		t.Fatal("dry run incorrect")
	}
	must(t, kube.Delete(ctx, p))
	_, e := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)})
	must(t, e)
	if replicas(t, p) != 1 {
		t.Fatal("policy deletion changed target")
	}
}
func TestCRDRejectsInvalidSpec(t *testing.T) {
	p, _, _, _ := setup(t)
	for _, change := range []func(*api.WorkloadScalingPolicy){func(p *api.WorkloadScalingPolicy) { p.Spec.MaxReplicas = 0 }, func(p *api.WorkloadScalingPolicy) { p.Spec.MinReplicas = 21 }, func(p *api.WorkloadScalingPolicy) { p.Spec.ScaleTargetRef.Kind = "StatefulSet" }, func(p *api.WorkloadScalingPolicy) { p.Spec.Metrics.Redis.Timeout.Duration = -time.Second }, func(p *api.WorkloadScalingPolicy) { p.Spec.Metrics.TargetQueueDepthPerReplica = 0 }, func(p *api.WorkloadScalingPolicy) {
		p.Spec.Schedules = []api.Schedule{{Name: "bad", TimeZone: "UTC", Days: []string{"Monday"}, StartTime: "25:00", EndTime: "26:00"}}
	}} {
		invalid := p.DeepCopy()
		invalid.Name = "invalid"
		invalid.ResourceVersion = ""
		invalid.UID = ""
		change(invalid)
		if e := kube.Create(ctx, invalid); !apierrors.IsInvalid(e) {
			t.Fatalf("invalid accepted or unexpected failure: %v", e)
		}
	}
}
func TestRuntimeTimezoneValidation(t *testing.T) {
	p, _, r, _ := setup(t)
	p.Spec.Schedules = []api.Schedule{{Name: "bad-zone", TimeZone: "Unknown/Zone", Days: []string{"Monday"}, StartTime: "09:00", EndTime: "18:00", MinReplicas: 1}}
	must(t, kube.Update(ctx, p))
	run(t, r, p)
	if !meta.IsStatusConditionTrue(p.Status.Conditions, "Degraded") || replicas(t, p) != 1 {
		t.Fatal("invalid zone did not fail closed")
	}
}

type conflictingClient struct {
	client.Client
	remaining int
	calls     int
}
type conflictingScale struct {
	client.SubResourceClient
	parent *conflictingClient
}

func (c *conflictingClient) SubResource(name string) client.SubResourceClient {
	return &conflictingScale{SubResourceClient: c.Client.SubResource(name), parent: c}
}
func (c *conflictingScale) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	c.parent.calls++
	if c.parent.remaining > 0 {
		c.parent.remaining--
		return apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments/scale"}, obj.GetName(), errors.New("injected resource version conflict"))
	}
	return c.SubResourceClient.Update(ctx, obj, opts...)
}
func TestOptimisticConflictRetriesPreserveFields(t *testing.T) {
	p, _, r, _ := setup(t)
	c := &conflictingClient{Client: kube, remaining: 1}
	r.Scaler = workload.Executor{Client: c, Reader: kube, Guard: r.Guard}
	run(t, r, p)
	if c.calls != 2 || replicas(t, p) != 2 {
		t.Fatal("retry not bounded correctly", c.calls)
	}
	var d appsv1.Deployment
	must(t, kube.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "worker"}, &d))
	if d.Annotations["preserve"] != "value" || d.Spec.Template.Spec.Containers[0].Image != "worker:dev" {
		t.Fatal("scale update overwrote unrelated fields")
	}
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
