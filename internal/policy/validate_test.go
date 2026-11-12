package policy

import (
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"github.com/scalepilot/scalepilot-operator/internal/testutil"
	"k8s.io/apimachinery/pkg/api/resource"
	"testing"
	"time"
)

func TestValidation(t *testing.T) {
	if e := Validate(testutil.Spec()); e != nil {
		t.Fatal(e)
	}
	for _, f := range []func(*api.WorkloadScalingPolicySpec){func(s *api.WorkloadScalingPolicySpec) { s.MinReplicas = -1 }, func(s *api.WorkloadScalingPolicySpec) { s.MaxReplicas = 0 }, func(s *api.WorkloadScalingPolicySpec) { s.ScaleTargetRef.Kind = "StatefulSet" }, func(s *api.WorkloadScalingPolicySpec) { s.Metrics.Redis.QueueName = "{injection}" }, func(s *api.WorkloadScalingPolicySpec) { s.Metrics.Redis.Timeout.Duration = -time.Second }, func(s *api.WorkloadScalingPolicySpec) {
		s.Schedules = []api.Schedule{{Name: "bad", TimeZone: "Mars/Olympus"}}
	}, func(s *api.WorkloadScalingPolicySpec) { s.MinReplicas = 0 }, func(s *api.WorkloadScalingPolicySpec) {
		q := resource.MustParse("-1")
		s.ResourcePolicy.MaxTotalCPU = &q
	}} {
		s := testutil.Spec()
		f(&s)
		if e := Validate(s); e == nil {
			t.Fatal("invalid spec accepted")
		}
	}
}
