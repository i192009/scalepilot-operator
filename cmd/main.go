package main

import (
	"context"
	"flag"
	"github.com/go-logr/logr"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"github.com/scalepilot/scalepilot-operator/internal/controller"
	"github.com/scalepilot/scalepilot-operator/internal/metrics"
	"github.com/scalepilot/scalepilot-operator/internal/observability"
	"github.com/scalepilot/scalepilot-operator/internal/security"
	"github.com/scalepilot/scalepilot-operator/internal/workload"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"log/slog"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"time"
	// +kubebuilder:scaffold:imports
)

func main() {
	var metricsAddr, probeAddr, namespace, leaderNamespace, logLevel string
	var leader, secureMetrics, clusterWide bool
	var admissionGuard bool
	var operatorIdentity string
	var interval time.Duration
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "metrics bind address")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "health bind address")
	flag.StringVar(&namespace, "watch-namespace", os.Getenv("WATCH_NAMESPACE"), "managed namespace; required unless --cluster-wide")
	flag.StringVar(&leaderNamespace, "leader-election-namespace", os.Getenv("POD_NAMESPACE"), "operator namespace for leader Lease")
	flag.StringVar(&logLevel, "log-level", "info", "debug, info, warn, error")
	flag.BoolVar(&leader, "leader-elect", true, "enable Lease-based leader election")
	flag.BoolVar(&secureMetrics, "metrics-secure", false, "HTTPS metrics with delegated Kubernetes authentication/authorization")
	flag.BoolVar(&clusterWide, "cluster-wide", false, "explicitly allow all namespaces")
	flag.BoolVar(&admissionGuard, "admission-guard", false, "enforce exclusive replica writes using admission TLS at :9443")
	flag.StringVar(&operatorIdentity, "operator-identity", "", "service account username authorized for replica writes")
	flag.DurationVar(&interval, "reconcile-interval", 5*time.Second, "fallback interval for invalid policies")
	flag.Parse()
	var level slog.Level
	if e := level.UnmarshalText([]byte(logLevel)); e != nil {
		panic(e)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))
	if namespace == "" && !clusterWide {
		logger.Error("--watch-namespace is required unless --cluster-wide is explicit")
		os.Exit(1)
	}
	scheme := runtime.NewScheme()
	must(clientgoscheme.AddToScheme(scheme))
	must(api.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
	cacheOptions := cache.Options{}
	if namespace != "" {
		cacheOptions.DefaultNamespaces = map[string]cache.Config{namespace: {}}
	}
	metricOptions := metricsserver.Options{BindAddress: metricsAddr, SecureServing: secureMetrics}
	if secureMetrics {
		metricOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}
	config := ctrl.GetConfigOrDie()
	config.QPS = 20
	config.Burst = 30
	mgr, e := ctrl.NewManager(config, ctrl.Options{Scheme: scheme, Cache: cacheOptions, Metrics: metricOptions, HealthProbeBindAddress: probeAddr, LeaderElection: leader, LeaderElectionID: "scalepilot-manager.scalepilot.io", LeaderElectionNamespace: leaderNamespace, LeaderElectionReleaseOnCancel: true, WebhookServer: webhook.NewServer(webhook.Options{Port: 9443, CertDir: "/etc/scalepilot/webhook"})})
	must(e)
	if admissionGuard {
		if operatorIdentity == "" {
			panic("--operator-identity required with --admission-guard")
		}
		server := mgr.GetWebhookServer()
		server.Register("/validate-replica-ownership", &admission.Webhook{Handler: &security.ScaleGuard{Reader: mgr.GetAPIReader(), OperatorIdentity: operatorIdentity}})
		must(mgr.AddReadyzCheck("webhook", server.StartedChecker()))
	}
	provider := &metrics.RedisProvider{Reader: mgr.GetAPIReader()}
	defer provider.Close()
	guard := workload.Guard{Reader: mgr.GetAPIReader(), Client: mgr.GetClient()}
	scaler := workload.Executor{Reader: mgr.GetAPIReader(), Client: mgr.GetClient(), Guard: guard}
	reconciler := &controller.WorkloadScalingPolicyReconciler{Client: mgr.GetClient(), Reader: mgr.GetAPIReader(), Scheme: scheme, Provider: provider, Guard: guard, Scaler: scaler, Recorder: observability.EventRecorder{Recorder: mgr.GetEventRecorder("scalepilot")}, DefaultInterval: interval}
	must(reconciler.SetupWithManager(mgr))
	// +kubebuilder:scaffold:builder
	must(mgr.AddHealthzCheck("healthz", healthz.Ping))
	must(mgr.AddReadyzCheck("readyz", healthz.Ping))
	stop, e := observability.SetupTracing(context.Background())
	must(e)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = stop(ctx)
	}()
	must(mgr.Start(ctrl.SetupSignalHandler()))
}
func must(e error) {
	if e != nil {
		slog.Error("fatal", "error", e)
		os.Exit(1)
	}
}
