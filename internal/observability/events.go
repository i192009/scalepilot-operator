package observability

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
)

type EventRecorder struct{ Recorder events.EventRecorder }

func (r EventRecorder) Event(object runtime.Object, kind, reason, message string) {
	r.Recorder.Eventf(object, nil, kind, reason, "Reconcile", "%s", message)
}
