#!/usr/bin/env bash
set -euo pipefail
case "$(kubectl config current-context)" in
  kind-*) ;;
  *) printf '%s\n' 'Metrics Server demo setup requires a Kind context.' >&2; exit 1 ;;
esac
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/download/v0.9.1/components.yaml
# Kind only. Never apply this certificate bypass to a production cluster.
metric_args=$(kubectl -n kube-system get deployment metrics-server -o jsonpath='{.spec.template.spec.containers[0].args}')
case "$metric_args" in
  *--kubelet-insecure-tls*) ;;
  *) kubectl -n kube-system patch deployment metrics-server --type=json -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]' ;;
esac
kubectl -n kube-system rollout status deployment/metrics-server --timeout=180s
