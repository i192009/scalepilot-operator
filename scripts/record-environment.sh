#!/usr/bin/env bash
set -euo pipefail
output=${1:-benchmarks/reports/environment.txt}
{
  date --utc --iso-8601=seconds
  uname -a
  go version
  kubectl version
  docker version --format '{{.Server.Version}}'
  docker info --format '{{.NCPU}} CPUs; {{.MemTotal}} bytes; {{.Architecture}}; {{.OperatingSystem}}'
  helm version --short
  kind version
  kubectl -n demo exec deployment/redis -- redis-cli INFO server | head -5
} > "$output"
