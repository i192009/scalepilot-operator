#!/usr/bin/env bash
set -euo pipefail
kubectl apply -f deployments/monitoring/stack.yaml
kubectl -n monitoring create configmap scalepilot-dashboard --from-file=dashboards/grafana/scalepilot.json --dry-run=client -o yaml | kubectl apply -f -
kubectl -n monitoring rollout status deployment/prometheus --timeout=180s
kubectl -n monitoring rollout status deployment/grafana --timeout=600s
kubectl -n monitoring rollout status deployment/kube-state-metrics --timeout=180s
