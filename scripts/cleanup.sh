#!/usr/bin/env bash
set -euo pipefail
# Only the dedicated ScalePilot Kind cluster is removed. Local source/reports remain.
kind delete cluster --name "${KIND_NAME:-scalepilot}"
