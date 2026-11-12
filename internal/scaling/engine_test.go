package scaling

import (
	"encoding/json"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"github.com/scalepilot/scalepilot-operator/internal/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
)

var fixed = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func input() Input {
	s := testutil.Spec()
	s.Behavior.StabilizationWindow.Duration = 0
	s.Behavior.Tolerance = 0
	return Input{Policy: s, Current: 2, Ready: 2, ResourceCeiling: 20, Now: fixed, Metrics: &api.QueueStatus{CollectedAt: metav1.NewTime(fixed)}}
}
func TestRecommendations(t *testing.T) {
	for _, tc := range []struct {
		name               string
		configure          func(*Input)
		recommend, desired int32
	}{
		{"depth", func(i *Input) { i.Metrics.Pending = 800; i.Policy.Metrics.WorkerThroughput = nil }, 8, 4},
		{"drain including arrivals", func(i *Input) {
			i.Metrics.Pending = 100
			i.Metrics.ArrivalRate = 10
			i.Policy.Metrics.TargetOldestJobAge.Duration = time.Hour
		}, 6, 4},
		{"overdue age", func(i *Input) { i.Metrics.Pending = 100; i.Metrics.OldestAgeSeconds = 31 }, 10, 4},
		{"measured latency", func(i *Input) {
			i.Metrics.Pending = 10
			i.Metrics.ExecutionSamples = 100
			i.Metrics.AverageProcessingSeconds = 2
			i.Metrics.P95ProcessingSeconds = 3
			i.Metrics.ArrivalRate = 2
		}, 6, 4},
		{"resource ceiling", func(i *Input) { i.Metrics.Pending = 100000; i.ResourceCeiling = 3 }, 3, 3},
		{"missing service rate", func(i *Input) { i.Metrics.Pending = 800; i.Policy.Metrics.WorkerThroughput = nil }, 8, 4},
		{"inflight protection", func(i *Input) { i.Metrics.InFlight = 2 }, 2, 2},
		{"readiness protection", func(i *Input) { i.Ready = 1 }, 1, 2},
		{"paused", func(i *Input) { i.Metrics.Pending = 800; i.Policy.Paused = true }, 14, 2},
		{"dry run", func(i *Input) { i.Metrics.Pending = 800; i.Policy.DryRun = true }, 14, 2},
		{"cooldown", func(i *Input) {
			i.Metrics.Pending = 800
			v := metav1.NewMicroTime(fixed.Add(-time.Second))
			i.Status.LastScaleTime = &v
		}, 14, 2},
		{"persisted intent", func(i *Input) {
			i.Metrics.Pending = 800
			i.Status.PendingScale = &api.PendingScale{Time: metav1.NewMicroTime(fixed.Add(-time.Second)), From: 1, To: 2}
		}, 14, 2},
		{"outlier bounded", func(i *Input) { i.Metrics.Pending = math.MaxInt64 - 1 }, 20, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := input()
			tc.configure(&i)
			d, e := Evaluate(i)
			if e != nil {
				t.Fatal(e)
			}
			if d.Recommended != tc.recommend || d.Desired != tc.desired {
				t.Fatalf("got recommended=%d desired=%d reason=%s; want %d,%d", d.Recommended, d.Desired, d.Reason, tc.recommend, tc.desired)
			}
		})
	}
}
func TestInvalidMetricsHold(t *testing.T) {
	for _, mutate := range []func(*Input){func(i *Input) { i.Metrics = nil }, func(i *Input) { i.Metrics.Pending = -1 }, func(i *Input) { i.Metrics.ArrivalRate = math.NaN() }, func(i *Input) { i.Metrics.P95ProcessingSeconds = math.Inf(1) }, func(i *Input) { i.Metrics.CollectedAt = metav1.NewTime(fixed.Add(-time.Hour)) }, func(i *Input) { i.Metrics.CollectedAt = metav1.NewTime(fixed.Add(time.Minute)) }, func(i *Input) { i.Metrics.ExecutionSamples = 1 }} {
		i := input()
		i.Current = 10
		mutate(&i)
		d, e := Evaluate(i)
		if e != nil || d.Desired != 10 || d.Reason != "MetricsUnavailable" {
			t.Fatalf("unsafe missing metric decision: %+v %v", d, e)
		}
	}
}

func TestMissingMetricFloorRespectsSafety(t *testing.T) {
	i := input()
	i.Metrics = nil
	i.Policy.MinReplicas = 10
	i.Policy.Safety.OnMetricsUnavailable = "ScaleToMinimum"
	d, e := Evaluate(i)
	if e != nil || d.Desired != 4 || d.Recommended != 10 {
		t.Fatal("floor bypassed step limits", d, e)
	}
	v := metav1.NewMicroTime(fixed.Add(-time.Second))
	i.Status.LastScaleTime = &v
	d, e = Evaluate(i)
	if e != nil || d.Desired != 2 {
		t.Fatal("floor bypassed persisted cooldown", d, e)
	}
	i.Current = 12
	d, e = Evaluate(i)
	if e != nil || d.Desired != 12 {
		t.Fatal("missing metrics authorized downscale", d, e)
	}
}
func TestStabilizationSurvivesJSONRestart(t *testing.T) {
	i := input()
	i.Current = 10
	i.Ready = 10
	i.Policy.Behavior.StabilizationWindow.Duration = time.Minute
	d, e := Evaluate(i)
	if e != nil {
		t.Fatal(e)
	}
	if d.Desired != 10 {
		t.Fatal("initial low demand must seed stabilization")
	}
	i.Status.Recommendations = d.History
	blob, _ := json.Marshal(i.Status)
	i.Status = api.WorkloadScalingPolicyStatus{}
	if e = json.Unmarshal(blob, &i.Status); e != nil {
		t.Fatal(e)
	}
	i.Now = fixed.Add(30 * time.Second)
	i.Metrics.CollectedAt = metav1.NewTime(i.Now)
	d, e = Evaluate(i)
	if e != nil || d.Desired != 10 {
		t.Fatal("restart lost stabilization", d, e)
	}
	i.Now = fixed.Add(61 * time.Second)
	i.Metrics.CollectedAt = metav1.NewTime(i.Now)
	d, e = Evaluate(i)
	if e != nil || d.Desired != 8 {
		t.Fatal("expired stabilization or step limit incorrect", d, e)
	}
}

func TestFractionalCooldownSurvivesJSONRestart(t *testing.T) {
	i := input()
	i.Metrics.Pending = 1000
	i.Policy.Behavior.ScaleUp.Cooldown.Duration = time.Second
	i.Policy.Behavior.MinimumInterval.Duration = time.Second
	attempted := metav1.NewMicroTime(fixed.Add(900 * time.Millisecond))
	i.Status.LastScaleAttemptTime = &attempted
	encoded, e := json.Marshal(i.Status)
	if e != nil {
		t.Fatal(e)
	}
	i.Status = api.WorkloadScalingPolicyStatus{}
	if e = json.Unmarshal(encoded, &i.Status); e != nil {
		t.Fatal(e)
	}
	i.Now = fixed.Add(1010 * time.Millisecond)
	i.Metrics.CollectedAt = metav1.NewTime(i.Now)
	d, e := Evaluate(i)
	if e != nil || d.Desired != 2 {
		t.Fatal("JSON timestamp precision shortened cooldown", d, e)
	}
	i.Now = fixed.Add(1901 * time.Millisecond)
	i.Metrics.CollectedAt = metav1.NewTime(i.Now)
	d, e = Evaluate(i)
	if e != nil || d.Desired <= 2 {
		t.Fatal("elapsed fractional cooldown did not release", d, e)
	}
}
func TestScaleToZeroActivationAndIdle(t *testing.T) {
	i := input()
	i.Policy.MinReplicas = 0
	i.Policy.Safety.AllowScaleToZero = true
	i.Current = 1
	i.Ready = 1
	d, _ := Evaluate(i)
	if d.Desired != 1 || d.IdleSince == nil {
		t.Fatal("cold idle period bypassed")
	}
	i.Status.IdleSince = d.IdleSince
	i.Now = fixed.Add(301 * time.Second)
	i.Metrics.CollectedAt = metav1.NewTime(i.Now)
	d, _ = Evaluate(i)
	if d.Desired != 0 {
		t.Fatal("did not scale to zero", d)
	}
	i.Current = 0
	i.Ready = 0
	i.Metrics.Pending = 1
	d, _ = Evaluate(i)
	if d.Desired < 1 {
		t.Fatal("external queue did not activate")
	}
}
func TestBoundsAndDeterminism(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		i := input()
		i.Current = int32(r.IntN(20) + 1)
		i.Ready = i.Current
		i.Metrics.Pending = int64(r.IntN(100000))
		i.ResourceCeiling = int32(r.IntN(20) + 1)
		if i.Current > i.ResourceCeiling {
			i.Current = i.ResourceCeiling
			i.Ready = i.Current
		}
		a, e := Evaluate(i)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := Evaluate(i)
		if !reflect.DeepEqual(a, b) || a.Desired < 1 || a.Desired > i.ResourceCeiling {
			t.Fatalf("invalid or nondeterministic: %+v %+v", a, b)
		}
	}
}
func TestOscillatingBacklog(t *testing.T) {
	i := input()
	i.Policy.Behavior.StabilizationWindow.Duration = time.Minute
	for k, pending := range []int64{1000, 100, 900, 50, 1200} {
		i.Now = fixed.Add(time.Duration(k) * 5 * time.Second)
		i.Metrics.CollectedAt = metav1.NewTime(i.Now)
		i.Metrics.Pending = pending
		d, e := Evaluate(i)
		if e != nil {
			t.Fatal(e)
		}
		if d.Desired < i.Current {
			t.Fatal("brief low demand caused downscale")
		}
		i.Status.Recommendations = d.History
		i.Current = d.Desired
		i.Ready = i.Current
	}
}
func BenchmarkEvaluate(b *testing.B) {
	i := input()
	i.Metrics.Pending = 10000
	for b.Loop() {
		if _, err := Evaluate(i); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkPolicySets(b *testing.B) {
	for _, count := range []int{10, 100, 500, 1000} {
		b.Run(fmtCount(count), func(b *testing.B) {
			inputs := make([]Input, count)
			for j := range inputs {
				inputs[j] = input()
				inputs[j].Metrics.Pending = int64(j * 100)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for _, i := range inputs {
					if _, e := Evaluate(i); e != nil {
						b.Fatal(e)
					}
				}
			}
		})
	}
}
func fmtCount(n int) string {
	if n == 10 {
		return "10"
	}
	if n == 100 {
		return "100"
	}
	if n == 500 {
		return "500"
	}
	return "1000"
}
