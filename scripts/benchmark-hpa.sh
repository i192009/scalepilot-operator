#!/usr/bin/env bash
set -euo pipefail
bash scripts/setup-metrics-server.sh
go run ./cmd/benchmark "$@"
