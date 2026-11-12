#!/usr/bin/env bash
# Run inside the Docker development container, with the isolated Kind demo ready.
set -euo pipefail
case "$(kubectl config current-context)" in kind-*) ;; *) echo 'Kind context required' >&2; exit 1;; esac
go build -o /tmp/scalepilot-benchmark ./cmd/benchmark
# Recreate the port-forward after Redis Pod replacement. A single forward exits
# with its selected Pod and cannot support an outage/recovery experiment.
forward_pid=''
forward_loop() {
  trap '[[ -z "$forward_pid" ]] || kill "$forward_pid" 2>/dev/null; exit' TERM INT
  while true; do
    kubectl -n demo port-forward svc/redis 16380:6379 >/tmp/scalepilot-benchmark-forward.log 2>&1 &
    forward_pid=$!
    wait "$forward_pid" || true
    sleep 1
  done
}
forward_loop &
supervisor_pid=$!
cleanup() {
  kill "$supervisor_pid" 2>/dev/null || true
  kubectl -n demo scale deployment/redis --replicas=1 || true
}
trap cleanup EXIT
for attempt in $(seq 1 30); do
  if timeout 2 bash -c 'exec 3<>/dev/tcp/127.0.0.1/16380' 2>/dev/null; then break; fi
  sleep 1
done
run() {
  /tmp/scalepilot-benchmark --redis-address=127.0.0.1:16380 "$@"
}
run --scenario=steady --jobs=100 --rate=5
run --scenario=ramp --jobs=100 --rate=5
run --scenario=burst --jobs=100 --rate=5
run --scenario=overload --jobs=100 --rate=20
run --scenario=spike --job-type=long --jobs=20 --job-duration=3s
run --scenario=redis-outage --jobs=200 --rate=10
run --scenario=worker-crash --jobs=200 --rate=10
run --scenario=controller-restart --jobs=200 --rate=10
