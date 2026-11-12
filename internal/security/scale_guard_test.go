package security

import (
	"context"
	"encoding/json"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"github.com/scalepilot/scalepilot-operator/internal/testutil"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"testing"
)

func TestReplicaAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, user                   string
		policies                     int
		paused, hpa, intent, allowed bool
	}{{name: "unmanaged", user: "hpa", allowed: true}, {name: "external blocked", user: "hpa", policies: 1}, {name: "duplicate blocked", user: "operator", policies: 2, intent: true}, {name: "paused permits handoff", user: "hpa", policies: 1, paused: true, allowed: true}, {name: "intent required", user: "operator", policies: 1}, {name: "exclusive intent", user: "operator", policies: 1, intent: true, allowed: true}, {name: "HPA creation fences operator", user: "operator", policies: 1, intent: true, hpa: true}} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = api.AddToScheme(scheme)
			_ = autoscalingv2.AddToScheme(scheme)
			var objects []client.Object
			for i := range tc.policies {
				p := &api.WorkloadScalingPolicy{ObjectMeta: metav1.ObjectMeta{Name: []string{"first", "second"}[i], Namespace: "demo", Generation: 1}, Spec: testutil.Spec()}
				p.Spec.Paused = tc.paused
				p.Status.ObservedGeneration = 1
				if tc.intent {
					p.Status.PendingScale = &api.PendingScale{From: 1, To: 2}
				}
				objects = append(objects, p)
			}
			if tc.hpa {
				objects = append(objects, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "hpa", Namespace: "demo"}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "worker"}}})
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			g := &ScaleGuard{Reader: c, OperatorIdentity: "operator"}
			old, _ := json.Marshal(autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: 1}})
			next, _ := json.Marshal(autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: 2}})
			response := g.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Name: "worker", Namespace: "demo", SubResource: "scale", OldObject: runtime.RawExtension{Raw: old}, Object: runtime.RawExtension{Raw: next}, UserInfo: authenticationv1.UserInfo{Username: tc.user}}})
			if response.Allowed != tc.allowed {
				t.Fatal("unexpected admission", response.Allowed, response.Result)
			}
		})
	}
}
