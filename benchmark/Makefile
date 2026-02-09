CLUSTER_NAME ?= volcano-benchmark
SCENARIO ?= gang
KWOK_NODE_COUNT ?= 100
CPU_PER_NODE ?= 32
MEMORY_PER_NODE ?= 256Gi
VOLCANO_VERSION ?=
TEST_CASE ?= $(SCENARIO)

export CLUSTER_NAME
export SCENARIO
export KWOK_NODE_COUNT
export CPU_PER_NODE
export MEMORY_PER_NODE

.PHONY: all
all: gang-all default-all cleanup-all

# Environment setup - requires SCENARIO to be set (e.g., make setup SCENARIO=gang)
# Can also install specific volcano version as make setup SCENARIO=gang VOLCANO_VERSION=v1.14.0
.PHONY: setup
setup:
	@echo "Setting up environment for scenario: $(SCENARIO)"
ifdef VOLCANO_VERSION
	$(MAKE) create-cluster
	$(MAKE) create-nodes SCENARIO=$(SCENARIO)
	$(MAKE) install-volcano SCENARIO=$(SCENARIO)
	$(MAKE) install-monitoring
else
	$(MAKE) create-cluster
	$(MAKE) create-nodes SCENARIO=$(SCENARIO)
	$(MAKE) build-images
	$(MAKE) install-volcano SCENARIO=$(SCENARIO)
	$(MAKE) install-monitoring
endif

.PHONY: create-cluster
create-cluster:
	bash scripts/create-cluster.sh

.PHONY: create-nodes
create-nodes:
	SCENARIO=$(SCENARIO) bash scripts/create-kwok-nodes.sh

.PHONY: build-images
build-images:
ifdef VOLCANO_VERSION
	@echo "Skipping image build (using release version $(VOLCANO_VERSION))"
else
	bash scripts/build-images.sh
endif

.PHONY: install-volcano
install-volcano:
ifdef VOLCANO_VERSION
	SCENARIO=$(SCENARIO) bash scripts/install-volcano.sh --release $(VOLCANO_VERSION)
else
	SCENARIO=$(SCENARIO) bash scripts/install-volcano.sh --local
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

.PHONY: cleanup
cleanup:
	bash scripts/cleanup.sh

.PHONY: cleanup-all
cleanup-all:
	bash scripts/cleanup.sh --delete-cluster

# Run predefined test cases
.PHONY: test
test:
	bash scripts/run-tests.sh $(TEST_CASE)

# Run ad-hoc CLI test
# Example: make test-cli SCENARIO=gang JOBS=10 PODS=100
.PHONY: test-cli
test-cli:
	bash scripts/run-tests.sh $(SCENARIO) --jobs=$(JOBS) --pods=$(PODS) $(if $(CPU),--cpu=$(CPU)) $(if $(MEMORY),--memory=$(MEMORY))

# Shortcut targets for predefined test cases
.PHONY: test-gang-20x50
test-gang-20x50:
	SCENARIO=gang bash scripts/run-tests.sh gang/case_20x50

.PHONY: test-gang-10x100
test-gang-10x100:
	SCENARIO=gang bash scripts/run-tests.sh gang/case_10x100

# Run all test cases in a scenario: setup -> test all cases -> cleanup
.PHONY: gang-all
gang-all:
	$(MAKE) setup SCENARIO=gang
	$(MAKE) test TEST_CASE=gang SCENARIO=gang
	$(MAKE) cleanup

.PHONY: default-all
default-all:
	$(MAKE) setup SCENARIO=default
	$(MAKE) test TEST_CASE=default SCENARIO=default
	$(MAKE) cleanup
