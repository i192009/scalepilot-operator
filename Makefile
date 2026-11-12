SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
GO ?= go
IMG ?= scalepilot/operator:dev
WORKER_IMG ?= scalepilot/worker:dev
LOADGEN_IMG ?= scalepilot/loadgen:dev
KIND_NAME ?= scalepilot
CONTROLLER_GEN_VERSION := v0.23.0
ENVTEST_VERSION := v0.25.3-0.20261103101737-9603adbb18be
ENVTEST_K8S_VERSION := 1.37.0
GOLANGCI_VERSION := v2.15.0
LOCALBIN := $(CURDIR)/bin
BIN_DIR ?= bin

.PHONY: all generate manifests fmt vet lint build docker-build test test-unit test-envtest test-race test-integration test-e2e kind-create kind-load install-crds deploy deploy-demo generate-load test-scaling test-failover test-conflict benchmark-hpa benchmark-matrix helm-lint monitoring-up undeploy kind-delete tools verify-generated benchmark
all: build
tools:
	mkdir -p $(LOCALBIN)
	GOBIN=$(LOCALBIN) $(GO) install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
	GOBIN=$(LOCALBIN) $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)
$(LOCALBIN)/controller-gen:
	mkdir -p $(LOCALBIN)
	GOBIN=$(LOCALBIN) $(GO) install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
$(LOCALBIN)/setup-envtest:
	mkdir -p $(LOCALBIN)
	GOBIN=$(LOCALBIN) $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)
generate: $(LOCALBIN)/controller-gen
	$(LOCALBIN)/controller-gen object:headerFile=hack/boilerplate.go.txt paths=./api/...
manifests: $(LOCALBIN)/controller-gen
	$(LOCALBIN)/controller-gen crd:allowDangerousTypes=true rbac:roleName=manager-role paths=./... output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac
	mkdir -p charts/scalepilot/crds charts/scalepilot/rbac
	cp config/crd/bases/*.yaml charts/scalepilot/crds/
	cp config/rbac/role.yaml charts/scalepilot/rbac/role.yaml
fmt:
	gofmt -w api cmd internal tests
vet:
	$(GO) vet ./...
lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run
lint-fix: fmt
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run --fix
build:
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN_DIR)/manager ./cmd
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN_DIR)/worker ./cmd/worker
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN_DIR)/loadgen ./cmd/loadgen
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN_DIR)/benchmark ./cmd/benchmark
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN_DIR)/policybench ./cmd/policybench
docker-build:
	docker build --target manager -t $(IMG) .
	docker build --target worker -t $(WORKER_IMG) .
	docker build --target loadgen -t $(LOADGEN_IMG) .
test: test-unit test-integration test-envtest
test-unit:
	$(GO) test ./internal/... ./cmd/... -count=1
test-race:
	$(GO) test -race ./internal/... ./cmd/... -count=1
test-envtest: manifests $(LOCALBIN)/setup-envtest
	KUBEBUILDER_ASSETS="$$($(LOCALBIN)/setup-envtest use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN)/k8s -p path)" $(GO) test -tags=envtest ./tests/envtest -count=1 -v -timeout=180s
test-integration:
	$(GO) test -tags=integration ./tests/integration -count=1 -v -timeout=180s
test-e2e test-scaling:
	$(GO) test -tags=e2e ./tests/e2e -count=1 -v -timeout=20m
test-failover:
	$(GO) test -tags=e2e ./tests/e2e -run TestFailover -count=1 -v -timeout=5m
test-conflict:
	$(GO) test -tags=e2e ./tests/e2e -run TestConflict -count=1 -v -timeout=5m
kind-create:
	kind create cluster --name $(KIND_NAME) --image kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5 --config deployments/kind/cluster.yaml --wait 120s
kind-load:
	kind load docker-image --name $(KIND_NAME) $(IMG) $(WORKER_IMG) $(LOADGEN_IMG)
install-crds: manifests
	kubectl apply --server-side --field-manager=scalepilot-crds --force-conflicts -f config/crd/bases
deploy:
	helm upgrade --install scalepilot charts/scalepilot --namespace scalepilot-system --create-namespace --wait --timeout 180s
deploy-demo:
	kubectl apply -f deployments/demo
	kubectl -n demo rollout status deployment/redis --timeout=180s
	kubectl -n demo rollout status deployment/document-worker --timeout=180s
generate-load:
	kubectl -n demo delete job generate-load --ignore-not-found
	kubectl apply -f examples/load-generator/job.yaml
helm-lint:
	helm lint charts/scalepilot
	helm template scalepilot charts/scalepilot --namespace scalepilot-system > /tmp/scalepilot-helm.yaml
monitoring-up:
	bash scripts/monitoring.sh
benchmark:
	mkdir -p benchmarks/reports
	$(GO) test ./internal/scaling -run '^$$' -bench . -benchmem -count=3 | tee benchmarks/reports/engine.txt
benchmark-hpa:
	bash scripts/benchmark-hpa.sh
benchmark-matrix:
	bash scripts/setup-metrics-server.sh
	bash scripts/benchmark-matrix.sh
verify-generated: generate manifests
	git diff --exit-code -- api/v1alpha1/zz_generated.deepcopy.go config/crd config/rbac/role.yaml charts/scalepilot/crds charts/scalepilot/rbac
undeploy:
	helm uninstall scalepilot --namespace scalepilot-system
	kubectl delete -f deployments/demo --ignore-not-found
kind-delete:
	kind delete cluster --name $(KIND_NAME)
