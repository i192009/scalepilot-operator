package main

import "k8s.io/apimachinery/pkg/util/intstr"

func intstrPort(n int) intstr.IntOrString { return intstr.FromInt(n) }
