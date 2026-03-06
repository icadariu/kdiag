CLUSTER_NAME    = kdiag-test
KUBECONFIG_FILE = /tmp/kdiag-test.kubeconfig
FIXTURES        = test/fixtures/kdiag-test.yaml

.PHONY: cluster-up cluster-down test test-unit test-integration build

## build: compile the binary
build:
	go build -o kdiag .

## test-unit: run unit tests (no cluster required)
test-unit:
	go test ./internal/...

## cluster-up: create a kind cluster and apply test fixtures
cluster-up:
	kind create cluster --name $(CLUSTER_NAME) --kubeconfig $(KUBECONFIG_FILE)
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl create namespace kdiag-test
	@echo "Waiting for default service account..."
	@until KUBECONFIG=$(KUBECONFIG_FILE) kubectl get serviceaccount default -n kdiag-test >/dev/null 2>&1; do sleep 1; done
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
	@echo "Triggering second rollout to create a previous ReplicaSet for rs diff tests..."
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl patch deployment test-app \
	  -n kdiag-test \
	  -p '{"spec":{"template":{"metadata":{"annotations":{"kdiag-rollout":"v2"}}}}}'
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl rollout status deployment/test-app \
	  -n kdiag-test --timeout=60s
	@echo "Cluster ready."

## cluster-down: delete the kind cluster
cluster-down:
	kind delete cluster --name $(CLUSTER_NAME)
	rm -f $(KUBECONFIG_FILE)

## test-integration: run integration tests against the kind cluster
test-integration:
	KUBECONFIG=$(KUBECONFIG_FILE) go test -v -tags integration ./test/ -timeout 120s

## test: run unit tests then integration tests
test: test-unit test-integration
