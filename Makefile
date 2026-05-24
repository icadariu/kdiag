CLUSTER_NAME    = kdiag-test
KUBECONFIG_FILE = /tmp/kdiag-test.kubeconfig
FIXTURES        = test/fixtures/kdiag-test.yaml

BIN        = kdiag
CMD_PKG    = .
VERSION    = $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILDTIME  = $(shell date -u +%d-%m-%y_%H:%M)
COMMIT     = $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS    = -X main.version=$(VERSION) -X main.buildTime=$(BUILDTIME) -X main.commit=$(COMMIT)
GOFLAGS    ?=

.PHONY: cluster-up cluster-down test unit-tests integration-tests build install autocompletion help

# Where to drop persisted shell-completion files. Override on the command line
# if your fpath / completion dir is elsewhere, e.g.:
#   make autocompletion ZSH_COMPLETIONS_DIR=~/.zfunc
ZSH_COMPLETIONS_DIR  ?= $(HOME)/.oh-my-zsh/plugins/kube-ps1
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

## cluster-up: create a kind cluster and apply test fixtures
cluster-up:
	kind create cluster --name $(CLUSTER_NAME) --kubeconfig $(KUBECONFIG_FILE)
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl create namespace kdiag-test
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
	@echo "Cluster ready."

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
