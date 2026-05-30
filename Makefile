CLUSTER_NAME    = kdiag-test
KUBECONFIG_FILE = /tmp/kdiag-test.kubeconfig
FIXTURES        = test/fixtures/kdiag-test.yaml
SCENARIOS       = test/fixtures/scenarios.yaml
KIND_CONFIG     = test/kind-config.yaml
# kind names workers <cluster>-worker, <cluster>-worker2, <cluster>-worker3.
# worker/worker2 stay schedulable; worker3 is the cordoned+tainted "broken node".
WORKER_A        = $(CLUSTER_NAME)-worker
WORKER_B        = $(CLUSTER_NAME)-worker2
WORKER_C        = $(CLUSTER_NAME)-worker3
KUBECTL         = KUBECONFIG=$(KUBECONFIG_FILE) kubectl

BIN        = kdiag
CMD_PKG    = .
# Branch-aware version stamping. On main, --dirty is honoured so release-ish
# builds still flag uncommitted edits. On any other branch (or detached HEAD),
# we suffix -dev to signal "not a release" and drop --dirty since -dev already
# implies dev-state.
BRANCH     = $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo dev)
ifeq ($(BRANCH),main)
VERSION    = $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
else
VERSION    = $(shell git describe --tags --always 2>/dev/null || echo dev)-dev
endif
BUILDTIME  = $(shell date -u +%d-%m-%y_%H:%M)
COMMIT     = $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS    = -X main.version=$(VERSION) -X main.buildTime=$(BUILDTIME) -X main.commit=$(COMMIT)
GOFLAGS    ?=

.PHONY: cluster-up cluster-down test unit-tests integration-tests build install autocompletion help

# Where to drop persisted shell-completion files. Override on the command line
# if your fpath / completion dir is elsewhere, e.g.:
#   make autocompletion ZSH_COMPLETIONS_DIR=~/.zsh/completions
# The zsh dir must be on your fpath before compinit runs — add this to .zshrc:
#   fpath=(~/.zfunc $fpath)
ZSH_COMPLETIONS_DIR  ?= $(HOME)/.zfunc
BASH_COMPLETIONS_DIR ?= $(HOME)/.local/share/bash-completion/completions

## help: show available make targets
help:
	@grep -E '^## ' Makefile | sed 's/^## //' | sort | column -t -s ':'

## build: build stamped binary into ./$(BIN)
build:
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN) $(CMD_PKG)

## install: install stamped binary into $GOBIN
install:
	go install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(CMD_PKG)

## autocompletion: write zsh/bash completions from the freshly-built binary and bust the zsh cache
autocompletion: build
	@mkdir -p $(ZSH_COMPLETIONS_DIR) $(BASH_COMPLETIONS_DIR)
	./$(BIN) completion zsh  > $(ZSH_COMPLETIONS_DIR)/_kdiag
	./$(BIN) completion bash > $(BASH_COMPLETIONS_DIR)/kdiag
	@rm -f $(HOME)/.zcompdump*
	@echo "Completions written. Restart your shell (or run 'exec zsh') to pick them up."

unit-tests:
	go test ./internal/...

## cluster-up: create a 3-node kind cluster, apply CI fixtures + manual scenarios
cluster-up:
	kind create cluster --name $(CLUSTER_NAME) --kubeconfig $(KUBECONFIG_FILE) --config $(KIND_CONFIG)
	@echo "Waiting for all nodes to be Ready..."
	$(KUBECTL) wait --for=condition=Ready nodes --all --timeout=120s
	@echo "Labelling workers with zones + instance types (so --az is meaningful)..."
	$(KUBECTL) label node $(WORKER_A) topology.kubernetes.io/zone=zone-a node.kubernetes.io/instance-type=kdiag.large --overwrite
	$(KUBECTL) label node $(WORKER_B) topology.kubernetes.io/zone=zone-b node.kubernetes.io/instance-type=kdiag.small --overwrite
	$(KUBECTL) label node $(WORKER_C) topology.kubernetes.io/zone=zone-c node.kubernetes.io/instance-type=kdiag.medium --overwrite
	$(KUBECTL) create namespace kdiag-test
	@echo "Waiting for default service account..."
	@until KUBECONFIG=$(KUBECONFIG_FILE) kubectl get serviceaccount default -n kdiag-test >/dev/null 2>&1; do sleep 1; done
	@# First apply lands the CRD (and most other resources). The Widget CR
	@# can race the CRD's API registration, so we ignore failures here, wait
	@# for the CRD to be Established, then re-apply (idempotent) to settle
	@# the CR.
	-KUBECONFIG=$(KUBECONFIG_FILE) kubectl apply -f $(FIXTURES)
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl wait --for=condition=Established crd/widgets.kdiag.test --timeout=60s
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl apply -f $(FIXTURES)
	@echo "Waiting for deployment pods to be ready..."
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl wait pod \
	  --for=condition=ready \
	  -l app=test-app \
	  -n kdiag-test \
	  --timeout=90s
	@echo "Waiting for static pod to be ready..."
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl wait pod/kdiag-static \
	  --for=condition=ready \
	  -n kdiag-test \
	  --timeout=90s
	@echo "Waiting for daemonset pods to be ready..."
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl rollout status daemonset/kdiag-ds \
	  -n kdiag-test --timeout=90s
	@echo "Waiting for statefulset pods to be ready..."
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl rollout status statefulset/kdiag-sts \
	  -n kdiag-test --timeout=90s
	@echo "Triggering rollouts so test-app has revisions 1, 2, 3 (needed for diff tests)..."
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl patch deployment test-app \
	  -n kdiag-test \
	  -p '{"spec":{"template":{"metadata":{"annotations":{"kdiag-rollout":"v2"}}}}}'
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl rollout status deployment/test-app \
	  -n kdiag-test --timeout=60s
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl patch deployment test-app \
	  -n kdiag-test \
	  -p '{"spec":{"template":{"metadata":{"annotations":{"kdiag-rollout":"v3"}}}}}'
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl rollout status deployment/test-app \
	  -n kdiag-test --timeout=60s
	@echo "Setting up the broken-node scenario (cordon + taint worker3; worker/worker2 stay schedulable)..."
	$(KUBECTL) cordon $(WORKER_C)
	$(KUBECTL) taint node $(WORKER_C) dedicated=special:NoSchedule --overwrite
	@echo "Applying manual-testing scenarios (broken pods expected — not waited on)..."
	$(KUBECTL) apply -f $(SCENARIOS)
	@echo ""
	@echo "Cluster ready. Manual-test playground (KUBECONFIG=$(KUBECONFIG_FILE)):"
	@echo "  kdiag inspect pod  --troubleshoot -n kdiag-scheduling   # sched-nodeselector / sched-cpu / sched-taint"
	@echo "  kdiag inspect pod  --troubleshoot -n kdiag-runtime      # rt-crashloop / rt-imagepull / rt-oom / rt-notready"
	@echo "  kdiag inspect deploy wl-degraded --troubleshoot -n kdiag-workloads   # Degraded"
	@echo "  kdiag inspect deploy wl-healthy  --troubleshoot -n kdiag-workloads   # Healthy"
	@echo "  kdiag inspect node --troubleshoot                       # cordoned + tainted workers"
	@echo "  kdiag inspect pod  --az -n kdiag-workloads --label app=wl-healthy    # zone spread"

## cluster-down: delete the kind cluster
cluster-down:
	kind delete cluster --name $(CLUSTER_NAME)
	rm -f $(KUBECONFIG_FILE)

integration-tests:
	@# Re-apply fixtures before each run. Kubernetes TTLs Events after
	@# ~1 hour, so on a long-running cluster the hand-crafted
	@# KdiagMultilineTest Event is gone and TestEvents_MultilineMessageSanitized
	@# fails. Re-applying is idempotent for the other resources.
	@# Two-phase apply because the Widget CR depends on the widgets CRD —
	@# on a fresh cluster the CR can race the CRD's API registration.
	-KUBECONFIG=$(KUBECONFIG_FILE) kubectl apply -f $(FIXTURES)
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl wait --for=condition=Established crd/widgets.kdiag.test --timeout=60s
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl apply -f $(FIXTURES)
	KUBECONFIG=$(KUBECONFIG_FILE) go test -v -tags integration ./test/ -timeout 120s

## test: run unit tests then integration tests
test: unit-tests integration-tests
