//go:build envtest

package envtest_test

import (
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func addLeaseScheme(s *runtime.Scheme) error { return coordinationv1.AddToScheme(s) }
