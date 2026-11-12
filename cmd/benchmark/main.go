// Command benchmark runs sequential equivalent workloads; it never synthesizes results.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"github.com/scalepilot/scalepilot-operator/internal/queue"
	"github.com/scalepilot/scalepilot-operator/internal/testutil"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Point struct {
	Seconds                       float64 `json:"seconds"`
	Pending, InFlight             int64
	Requested, Ready              int32
	CPUCore, MemoryBytes          *float64
	CollectionSeconds, APISeconds float64
}
type Report struct {
	Fault                                                                              string
	ProducerRetries                                                                    int64
	CollectionFailures                                                                 int
	Strategy, Scenario, JobType, Queue                                                 string
	Jobs                                                                               int
	MeanDurationMS                                                                     int64
	Started                                                                            time.Time
	DurationSeconds                                                                    float64
	ReactionSeconds                                                                    *float64
	MaxBacklog                                                                         int64
	MaxReplicas                                                                        int32
	PodSeconds, ReadyPodSeconds                                                        float64
	ScaleUps, ScaleDowns, Oscillations                                                 int
	Completed, Failed                                                                  int
	AverageWaitSeconds, P95WaitSeconds, AverageProcessingSeconds, P95ProcessingSeconds float64
	AverageCPUCore, AverageMemoryBytes                                                 *float64
	ResourceSamples                                                                    int
	Series                                                                             []Point
}

func main() {
	var scenario, kind, dir, address string
	var jobs, repeats int
	var duration time.Duration
	var rate float64
	flag.StringVar(&scenario, "scenario", "spike", "steady, spike, ramp, burst, overload, redis-outage, worker-crash, controller-restart")
	flag.StringVar(&kind, "job-type", "io", "io or cpu (also fast/medium/long)")
	flag.StringVar(&dir, "output", "benchmarks/reports", "report directory")
	flag.StringVar(&address, "redis-address", "127.0.0.1:16379", "Redis endpoint, optionally port-forwarded")
	flag.IntVar(&jobs, "jobs", 250, "equivalent jobs per arm (1..9000)")
	flag.IntVar(&repeats, "repeats", 1, "repeated sequential pairs")
	flag.DurationVar(&duration, "job-duration", 400*time.Millisecond, "mean job execution time")
	flag.Float64Var(&rate, "rate", 20, "base jobs/sec for non-spike patterns")
	flag.Parse()
	selectedContext, contextErr := exec.Command("kubectl", "config", "current-context").Output()
	must(contextErr)
	if !strings.HasPrefix(strings.TrimSpace(string(selectedContext)), "kind-") {
		panic("benchmarks require an isolated Kind context")
	}
	if jobs < 1 || jobs > 9000 || repeats < 1 || rate <= 0 || duration < time.Millisecond {
		panic("invalid benchmark settings")
	}
	switch scenario {
	case "steady", "spike", "ramp", "burst", "overload", "redis-outage", "worker-crash", "controller-restart":
	default:
		panic("unknown scenario")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	scheme := runtime.NewScheme()
	must(api.AddToScheme(scheme))
	must(appsv1.AddToScheme(scheme))
	must(corev1.AddToScheme(scheme))
	must(autoscalingv2.AddToScheme(scheme))
	must(coordinationv1.AddToScheme(scheme))
	c, e := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	must(e)
	r, e := queue.NewClient(map[string][]byte{"address": []byte(address)}, false, "")
	must(e)
	defer func() { _ = r.Close() }()
	must(os.MkdirAll(dir, 0755))
	activeStrategy, activeName := "", ""
	defer func() {
		if activeName != "" {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if activeStrategy == "hpa" {
				_ = c.Delete(cleanup, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: activeName, Namespace: "demo"}})
			} else {
				_ = c.Delete(cleanup, &api.WorkloadScalingPolicy{ObjectMeta: metav1.ObjectMeta{Name: activeName, Namespace: "demo"}})
			}
			_ = c.Delete(cleanup, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: activeName, Namespace: "demo"}})
		}
	}()
	for run := 1; run <= repeats; run++ {
		for _, strategy := range []string{"hpa", "pilot"} {
			name := strategy + "-worker"
			queueName := fmt.Sprintf("bench-%s-%s-%d-%d", strategy, scenario, run, time.Now().Unix())
			q := queue.Queue{Client: r, Name: queueName, Group: "workers"}
			must(q.Ensure(ctx))
			must(createArm(ctx, c, strategy, name, queueName))
			activeStrategy, activeName = strategy, name
			waitReady(ctx, c, name)
			// Warm both arms equally, allowing Metrics Server/HPA to observe the initial worker.
			time.Sleep(30 * time.Second)
			report := Report{Strategy: strategy, Scenario: scenario, JobType: kind, Queue: queueName, Jobs: jobs, MeanDurationMS: duration.Milliseconds(), Started: time.Now().UTC()}
			producerDone := make(chan error, 1)
			var producerRetries atomic.Int64
			go func() {
				rng := rand.New(rand.NewPCG(42, 43))
				for j := range jobs {
					if scenario != "spike" {
						mult := 1.0
						elapsed := time.Since(report.Started).Seconds()
						switch scenario {
						case "ramp":
							mult = 1 + 9*float64(j)/float64(jobs)
						case "burst":
							if int(elapsed/10)%2 == 1 {
								select {
								case <-ctx.Done():
									producerDone <- ctx.Err()
									return
								case <-time.After(10 * time.Second):
								}
							}
						case "overload":
							mult = 10
						}
						select {
						case <-ctx.Done():
							producerDone <- ctx.Err()
							return
						case <-time.After(time.Duration(float64(time.Second) / rate / mult)):
						}
					}
					job := queue.Job{ID: queueName + "-" + strconv.Itoa(j), Kind: kind, DurationMS: max(1, duration.Milliseconds()*int64(75+rng.IntN(51))/100)}
					retryCtx, retryCancel := context.WithTimeout(ctx, time.Minute)
					_, err := q.Enqueue(retryCtx, job)
					for err != nil && retryCtx.Err() == nil {
						producerRetries.Add(1)
						select {
						case <-retryCtx.Done():
						case <-time.After(time.Second):
							_, err = q.Enqueue(retryCtx, job)
						}
					}
					retryCancel()
					if err != nil {
						producerDone <- err
						return
					}
				}
				producerDone <- nil
			}()
			finished := false
			last := report.Started
			previous := int32(1)
			lastDirection := 0
			injected := false
			cpuTotal, memoryTotal := 0.0, 0.0
			for time.Since(report.Started) < 15*time.Minute {
				// Completion must be observed BEFORE the queue snapshot. Otherwise
				// a snapshot taken before enqueue can be mistaken for a drained
				// queue when the producer finishes during subsequent API calls.
				if !finished {
					select {
					case err := <-producerDone:
						must(err)
						finished = true
					default:
					}
				}
				started := time.Now()
				m, err := q.Snapshot(ctx)
				if err != nil {
					report.CollectionFailures++
					time.Sleep(time.Second)
					continue
				}
				collection := time.Since(started).Seconds()
				var d appsv1.Deployment
				started = time.Now()
				must(c.Get(ctx, client.ObjectKey{Namespace: "demo", Name: name}, &d))
				apiSeconds := time.Since(started).Seconds()
				now := time.Now()
				seconds := now.Sub(report.Started).Seconds()
				cpu, mem := usage(name)
				report.Series = append(report.Series, Point{Seconds: seconds, Pending: m.Pending, InFlight: m.InFlight, Requested: *d.Spec.Replicas, Ready: d.Status.ReadyReplicas, CPUCore: cpu, MemoryBytes: mem, CollectionSeconds: collection, APISeconds: apiSeconds})
				report.PodSeconds += float64(d.Status.Replicas) * now.Sub(last).Seconds()
				report.ReadyPodSeconds += float64(d.Status.ReadyReplicas) * now.Sub(last).Seconds()
				last = now
				report.MaxBacklog = max(report.MaxBacklog, m.Pending+m.InFlight)
				report.MaxReplicas = max(report.MaxReplicas, *d.Spec.Replicas)
				if cpu != nil && mem != nil {
					cpuTotal += *cpu
					memoryTotal += *mem
					report.ResourceSamples++
				}
				direction := 0
				if *d.Spec.Replicas > previous {
					report.ScaleUps++
					direction = 1
					if report.ReactionSeconds == nil {
						v := seconds
						report.ReactionSeconds = &v
					}
				} else if *d.Spec.Replicas < previous {
					report.ScaleDowns++
					direction = -1
				}
				if direction != 0 {
					if lastDirection != 0 && lastDirection != direction {
						report.Oscillations++
					}
					lastDirection = direction
				}
				previous = *d.Spec.Replicas
				if !injected && seconds >= 10 {
					switch scenario {
					case "worker-crash":
						runKubectl("-n", "demo", "delete", "pods", "-l", "app="+name, "--grace-period=0", "--force")
						report.Fault = "force-deleted this arm's worker Pods at >=10 seconds"
					case "controller-restart":
						report.Fault = restartController(ctx, c, strategy)
					case "redis-outage":
						runKubectl("-n", "demo", "scale", "deployment/redis", "--replicas=0")
						time.Sleep(10 * time.Second)
						report.Fault = "Redis scaled to zero for 10 seconds, then restored; includes restart time"
						runKubectl("-n", "demo", "scale", "deployment/redis", "--replicas=1")
						time.Sleep(10 * time.Second)
					}
					injected = true
				}
				if finished && m.Pending == 0 && m.InFlight == 0 {
					break
				}
				time.Sleep(time.Second)
			}
			report.DurationSeconds = time.Since(report.Started).Seconds()
			report.ProducerRetries = producerRetries.Load()
			if report.ResourceSamples > 0 {
				cpu := cpuTotal / float64(report.ResourceSamples)
				memory := memoryTotal / float64(report.ResourceSamples)
				report.AverageCPUCore = &cpu
				report.AverageMemoryBytes = &memory
			}
			results, e := r.XRange(ctx, q.Key("results"), "-", "+").Result()
			must(e)
			var waits, processing []float64
			for _, entry := range results {
				if entry.Values["state"] == "failed" {
					report.Failed++
					continue
				}
				report.Completed++
				w, _ := strconv.ParseFloat(fmt.Sprint(entry.Values["wait"]), 64)
				p, _ := strconv.ParseFloat(fmt.Sprint(entry.Values["duration"]), 64)
				waits = append(waits, w)
				processing = append(processing, p)
			}
			if report.Completed+report.Failed != jobs {
				panic(fmt.Sprintf("incomplete or truncated result audit: %d/%d", report.Completed+report.Failed, jobs))
			}
			report.AverageWaitSeconds, report.P95WaitSeconds = stats(waits)
			report.AverageProcessingSeconds, report.P95ProcessingSeconds = stats(processing)
			path := filepath.Join(dir, fmt.Sprintf("%s-%s-%s-%d.json", strategy, scenario, kind, run))
			b, _ := json.MarshalIndent(report, "", "  ")
			must(os.WriteFile(path, b, 0644))
			fmt.Printf("%s: drained=%0.2fs maxReplicas=%d p95Wait=%.2fs podSeconds=%.2f -> %s\n", strategy, report.DurationSeconds, report.MaxReplicas, report.P95WaitSeconds, report.PodSeconds, path)
			cleanupArm(ctx, c, strategy, name)
			activeName = ""
		}
	}
}
func createArm(ctx context.Context, c client.Client, strategy, name, queueName string) error {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "demo"}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To(int32(1)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}}, Spec: corev1.PodSpec{TerminationGracePeriodSeconds: ptr.To(int64(35)), Containers: []corev1.Container{{Name: "worker", Image: "scalepilot/worker:dev", ImagePullPolicy: corev1.PullIfNotPresent, Args: []string{"--queue=" + queueName}, Env: []corev1.EnvVar{{Name: "REDIS_ADDRESS", Value: "redis.demo.svc.cluster.local:6379"}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("128Mi")}}, ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstrPort(8081)}}}}}}}}}
	if e := c.Create(ctx, d); e != nil {
		return e
	}
	if strategy == "hpa" {
		return c.Create(ctx, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "demo"}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: name}, MinReplicas: ptr.To(int32(1)), MaxReplicas: 6, Metrics: []autoscalingv2.MetricSpec{{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: ptr.To(int32(60))}}}}}})
	}
	s := testutil.Spec()
	s.ScaleTargetRef.Name = name
	s.MaxReplicas = 6
	s.Metrics.Redis.SecretRef.Name = "redis-connection"
	s.Metrics.Redis.QueueName = queueName
	s.Metrics.TargetQueueDepthPerReplica = 50
	s.Behavior.StabilizationWindow.Duration = 15 * time.Second
	s.Behavior.ScaleDown.Cooldown.Duration = 10 * time.Second
	return c.Create(ctx, &api.WorkloadScalingPolicy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "demo"}, Spec: s})
}
func cleanupArm(ctx context.Context, c client.Client, strategy, name string) {
	if strategy == "hpa" {
		must(c.Delete(ctx, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "demo"}}))
	} else {
		must(c.Delete(ctx, &api.WorkloadScalingPolicy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "demo"}}))
	}
	must(c.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "demo"}}))
	time.Sleep(5 * time.Second)
}
func waitReady(ctx context.Context, c client.Client, name string) {
	for range 90 {
		var d appsv1.Deployment
		must(c.Get(ctx, client.ObjectKey{Name: name, Namespace: "demo"}, &d))
		if d.Status.ReadyReplicas == 1 {
			return
		}
		time.Sleep(time.Second)
	}
	panic("benchmark worker never ready")
}
func stats(v []float64) (float64, float64) {
	if len(v) == 0 {
		return 0, 0
	}
	sort.Float64s(v)
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v)), v[(len(v)*95+99)/100-1]
}
func usage(name string) (*float64, *float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, e := exec.CommandContext(ctx, "kubectl", "get", "--raw", "/apis/metrics.k8s.io/v1beta1/namespaces/demo/pods").Output()
	if e != nil {
		return nil, nil
	}
	var data struct {
		Items []struct {
			Metadata   struct{ Name string }
			Containers []struct{ Usage map[string]string }
		}
	}
	if json.Unmarshal(out, &data) != nil {
		return nil, nil
	}
	cpu, mem := 0.0, 0.0
	matched := false
	for _, p := range data.Items {
		if !strings.HasPrefix(p.Metadata.Name, name+"-") {
			continue
		}
		for _, c := range p.Containers {
			matched = true
			q, e := resource.ParseQuantity(c.Usage["cpu"])
			if e != nil {
				return nil, nil
			}
			cpu += q.AsApproximateFloat64()
			q, e = resource.ParseQuantity(c.Usage["memory"])
			if e != nil {
				return nil, nil
			}
			mem += q.AsApproximateFloat64()
		}
	}
	if !matched {
		return nil, nil
	}
	return &cpu, &mem
}
func runKubectl(args ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, e := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput(); e != nil {
		panic(fmt.Sprintf("kubectl failed: %s %v", out, e))
	}
}
func restartController(ctx context.Context, c client.Client, strategy string) string {
	if strategy == "pilot" {
		var lease coordinationv1.Lease
		must(c.Get(ctx, client.ObjectKey{Namespace: "scalepilot-system", Name: "scalepilot-manager.scalepilot.io"}, &lease))
		if lease.Spec.HolderIdentity == nil {
			panic("no active ScalePilot leader")
		}
		pod := strings.SplitN(*lease.Spec.HolderIdentity, "_", 2)[0]
		runKubectl("-n", "scalepilot-system", "delete", "pod", pod, "--grace-period=0", "--force")
		return "force-deleted active ScalePilot leader " + pod + " (two manager replicas)"
	}
	// Kind's kube-controller-manager is a static Pod. Deleting its mirror Pod
	// does not restart the process; stopping its CRI container does.
	var nodes corev1.NodeList
	must(c.List(ctx, &nodes, client.MatchingLabels{"node-role.kubernetes.io/control-plane": ""}))
	if len(nodes.Items) != 1 {
		panic("controller restart requires a single-control-plane Kind cluster")
	}
	node := nodes.Items[0].Name
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(commandCtx, "docker", "exec", node, "crictl", "ps", "--name", "kube-controller-manager", "-q").Output()
	must(err)
	id := strings.TrimSpace(string(out))
	if id == "" || len(strings.Fields(id)) != 1 {
		panic("expected one running kube-controller-manager container")
	}
	_, err = exec.CommandContext(commandCtx, "docker", "exec", node, "crictl", "stop", id).CombinedOutput()
	must(err)
	return "stopped Kind kube-controller-manager CRI container " + id + "; kubelet restarts the static Pod (also affects other built-in controllers)"
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}
