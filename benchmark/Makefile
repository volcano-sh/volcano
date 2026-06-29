CLUSTER_NAME ?= volcano-benchmark
SCENARIO ?= gang
KWOK_NODE_COUNT ?= 100
CPU_PER_NODE ?= 32
MEMORY_PER_NODE ?= 256Gi
VOLCANO_VERSION ?=
CONFIG ?= basic.yaml
USE_EXISTING_CLUSTER ?= false
SKIP_INSTALL_VOLCANO ?= false
SKIP_INSTALL_MONITORING ?= false
SKIP_KWOK ?= false
EXCLUDE_SYSTEM_DAEMONSETS_FROM_KWOK ?= true
DISABLE_KWOK_POD_COMPLETE ?= true
AUDIT_EXPORTER_IMAGE ?= volcanosh/kube-apiserver-audit-exporter:dev
VOLCANO_SCHEDULER_KUBE_API_QPS ?= 5000
VOLCANO_SCHEDULER_KUBE_API_BURST ?= 10000
VOLCANO_CONTROLLER_KUBE_API_QPS ?= 5000
VOLCANO_CONTROLLER_KUBE_API_BURST ?= 10000

export CLUSTER_NAME
export KWOK_NODE_COUNT
export CPU_PER_NODE
export MEMORY_PER_NODE
export USE_EXISTING_CLUSTER
export SKIP_INSTALL_VOLCANO
export SKIP_INSTALL_MONITORING
export SKIP_KWOK
export EXCLUDE_SYSTEM_DAEMONSETS_FROM_KWOK
export DISABLE_KWOK_POD_COMPLETE
export AUDIT_EXPORTER_IMAGE
export VOLCANO_SCHEDULER_KUBE_API_QPS
export VOLCANO_SCHEDULER_KUBE_API_BURST
export VOLCANO_CONTROLLER_KUBE_API_QPS
export VOLCANO_CONTROLLER_KUBE_API_BURST

# Environment setup, scenario-independent infrastructure.
# Can also install specific volcano version: make setup VOLCANO_VERSION=v1.14.0
.PHONY: setup
setup:
	@echo "Setting up benchmark infrastructure..."
ifeq ($(USE_EXISTING_CLUSTER),true)
	@echo "Using existing cluster (USE_EXISTING_CLUSTER=true), skipping Kind cluster creation and image build..."
else
	$(MAKE) create-cluster
	$(MAKE) build-images
endif
ifeq ($(SKIP_KWOK),true)
	@echo "Skipping KWOK node creation (SKIP_KWOK=true), using existing cluster nodes..."
else
	$(MAKE) create-nodes
endif
ifeq ($(SKIP_INSTALL_VOLCANO),true)
	@echo "Skipping Volcano installation (SKIP_INSTALL_VOLCANO=true), using pre-installed Volcano..."
else
	$(MAKE) install-volcano
endif
ifeq ($(SKIP_INSTALL_MONITORING),true)
	@echo "Skipping monitoring installation (SKIP_INSTALL_MONITORING=true)..."
else
	$(MAKE) install-monitoring
endif

.PHONY: create-cluster
create-cluster:
	bash scripts/create-cluster.sh

.PHONY: create-nodes
create-nodes:
	bash scripts/create-kwok-nodes.sh

.PHONY: create-hypernodes
create-hypernodes:
	bash scripts/create-hypernodes.sh

.PHONY: build-images
build-images:
ifdef VOLCANO_VERSION
	@echo "Using release version $(VOLCANO_VERSION), building audit-exporter only"
	bash scripts/build-images.sh
else
	BUILD_VOLCANO=true bash scripts/build-images.sh
endif

# Build audit-exporter image only (for existing clusters that need the image pushed to a registry)
.PHONY: build-audit-exporter
build-audit-exporter:
	@echo "Building audit-exporter image ($(AUDIT_EXPORTER_IMAGE))..."
	docker build \
		-f manifests/audit-exporter/Dockerfile \
		-t $(AUDIT_EXPORTER_IMAGE) \
		$(shell cd .. && pwd)

.PHONY: install-volcano
install-volcano:
ifdef VOLCANO_VERSION
	bash scripts/install-volcano.sh --release $(VOLCANO_VERSION)
else
	bash scripts/install-volcano.sh --local
endif

.PHONY: install-monitoring
install-monitoring:
	bash scripts/install-monitoring.sh

.PHONY: export-charts
export-charts:
	bash scripts/export-grafana-charts.sh

.PHONY: report
report:
	bash scripts/collect-report.sh

# Collect scheduling latency from pod timestamps (for use with DRY_RUN=true, no audit-exporter needed)
.PHONY: collect-pod-latency
collect-pod-latency:
	bash scripts/collect-pod-latency.sh

.PHONY: cleanup
cleanup:
	bash scripts/cleanup.sh

.PHONY: cleanup-all
cleanup-all:
	bash scripts/cleanup.sh --delete-cluster

# Run custom test using a yaml configuration profile
# SCENARIO determines which scheduler config + queue to apply before running.
# DRY_RUN=true can be used to skip cleanup
# Example: make test-config SCENARIO=gang CONFIG=basic.yaml
.PHONY: test-config
test-config:
	bash scripts/run-tests.sh $(SCENARIO) --config=$(CONFIG)

# Run basic gang scheduling tests with configurable parameters
# This target uses cases/case-template.yaml to specify Job counts, Replicas, and MinAvailable.
# DRY_RUN=true can be used to skip cleanup
# Usage: make test-gang-env JOBS=10 REPLICAS=100 MIN_AVAILABLE=100
.PHONY: test-gang-env
test-gang-env:
	$(eval JOBS ?= 10)
	$(eval REPLICAS ?= 100)
	$(eval MIN_AVAILABLE ?= 100)
	JOBS=$(JOBS) REPLICAS=$(REPLICAS) MIN_AVAILABLE=$(MIN_AVAILABLE) bash scripts/run-tests.sh gang --template=cases/case-template.yaml

.PHONY: clean-vcjobs
clean-vcjobs:
	kubectl delete vcjob -l volcano.sh/benchmark=true -A --ignore-not-found
	kubectl delete pods -l volcano.sh/benchmark=true -A --ignore-not-found

.PHONY: clean-pods
clean-pods:
	kubectl delete pods -l volcano.sh/benchmark=true -A --ignore-not-found

# Run bare pod scheduling tests with configurable parameters
# Parameters:
#   PODS: Number of bare pods to create (default: 500)
#   SCHEDULER_NAME: The scheduler to benchmark (default: agent-scheduler)
# Usage: make test-pod-env PODS=500 SCHEDULER_NAME=agent-scheduler
.PHONY: test-pod-env
test-pod-env:
	$(eval PODS ?= 500)
	$(eval SCHEDULER_NAME ?= agent-scheduler)
	PODS=$(PODS) SCHEDULER_NAME=$(SCHEDULER_NAME) bash scripts/run-tests.sh pod --template=cases/case-template.yaml
