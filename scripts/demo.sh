#!/usr/bin/env bash
set -euo pipefail
make kind-create docker-build kind-load install-crds deploy deploy-demo monitoring-up
make generate-load
make test-e2e
