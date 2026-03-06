# Copyright 2025 The Volcano Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This Makefile expects the following variables to be defined by the parent/root Makefile:
# OUTPUT_DIR, BIN_DIR
ifndef OUTPUT_DIR
$(error OUTPUT_DIR is not set. Please define it in the parent Makefile.)
endif

ifndef BIN_DIR
$(error BIN_DIR is not set. Please define it in the parent Makefile.)
endif


# Map host CPU to release archive naming: aarch64 becomes arm64 to match
# upstream tarball names. x86_64 is intentionally kept as-is because Tilt
# and ctlptl archives use "linux.x86_64" (not "amd64") in their filenames.
HOSTARCH := $(shell uname -m)
ifeq ($(HOSTARCH),aarch64)
HOSTARCH := arm64
endif

TILT_VERSION=0.36.3
CTLPTL_VERSION ?= 0.9.0
TILT_BIN=${BIN_DIR}/tilt
KIND_BIN=${BIN_DIR}/kind
CTLPTL_BIN=${BIN_DIR}/ctlptl

ifndef KIND_VERSION
$(error KIND_VERSION is not set. Please define it in the parent Makefile.)
endif

KIND_CLUSTER_NAME ?= kind-volcano-dev
KIND_CONTEXT_NAME ?= ${KIND_CLUSTER_NAME}
CTLPTL_CONFIG ?= hack/tilt/ctlptl-kind-registry.yaml
KIND_CONFIG ?= hack/e2e-kind-config.yaml

TILT_WAIT_TIMEOUT ?= 10m
TILT_WAIT_RESOURCES ?= \
	uiresource/volcano-namespaces \
	uiresource/volcano-crds \
	uiresource/volcano-vcjob-roles \
	uiresource/volcano-admission-init \
	uiresource/volcano-admission \
	uiresource/volcano-scheduler \
	uiresource/volcano-controllers \
	uiresource/prometheus-deployment \
	uiresource/kube-state-metrics \
	uiresource/grafana

# Install Kind if not present in _output/bin or wrong version
dev-install-kind:
	@PATH="${BIN_DIR}:$$PATH" KIND_BIN="${KIND_BIN}" KIND_VERSION="${KIND_VERSION}" bash -c 'source hack/lib/install.sh; check-kind'

# Install ctlptl if not present in _output/bin or wrong version
dev-install-ctlptl:
	@if [ ! -f ${CTLPTL_BIN} ] || [ "$$(${CTLPTL_BIN} version 2>/dev/null | grep -Eo '[0-9]+\.[0-9]+\.[0-9]+' | head -1)" != "${CTLPTL_VERSION}" ]; then \
		mkdir -p ${BIN_DIR}; \
		curl -fsSL https://github.com/tilt-dev/ctlptl/releases/download/v${CTLPTL_VERSION}/ctlptl.${CTLPTL_VERSION}.linux.${HOSTARCH}.tar.gz | tar -xz -C ${BIN_DIR} ctlptl; \
		chmod +x ${CTLPTL_BIN}; \
		printf "ctlptl installed at ${CTLPTL_BIN} (version ${CTLPTL_VERSION}).\n"; \
	else \
		printf "ctlptl already installed at ${CTLPTL_BIN} (version ${CTLPTL_VERSION}).\n"; \
	fi

# Create Kind cluster and registry for local dev via ctlptl.
# Composes the ctlptl config with the shared kind cluster config at apply time
# so that e2e-kind-config.yaml remains the single source of truth.
dev-create-kind-cluster:
	@{ \
	  cat ${CTLPTL_CONFIG}; \
	  printf 'kindV1Alpha4Cluster:\n'; \
	  printf '  name: %s\n' '${KIND_CLUSTER_NAME}'; \
	  sed -e '/^kind:/d' -e '/^apiVersion:/d' -e '/^[[:space:]]*$$/d' -e '/^#/d' -e 's/^/  /' ${KIND_CONFIG}; \
	} | ${CTLPTL_BIN} apply -f -

.PHONY: dev-up
dev-up: dev-install-tilt dev-install-kind dev-install-ctlptl
	$(MAKE) dev-create-kind-cluster
	$(TILT_BIN) up -f hack/tilt/Tiltfile

.PHONY: dev-down
dev-down:
	@$(TILT_BIN) down -f hack/tilt/Tiltfile 2>/dev/null || true
	@pkill -f '^${TILT_BIN} up -f hack/tilt/Tiltfile$$' || true

# Delete Kind cluster and registry managed by ctlptl
.PHONY: dev-delete-kind-cluster
dev-delete-kind-cluster:
	@${CTLPTL_BIN} delete -f ${CTLPTL_CONFIG} --cascade=true --ignore-not-found || true

.PHONY: dev-clean
dev-clean: dev-down dev-delete-kind-cluster
	rm -f $(KIND_BIN) $(CTLPTL_BIN) $(TILT_BIN)

.PHONY: dev-status
dev-status:
	${CTLPTL_BIN} get cluster
	@echo ---
	@kubectl cluster-info --context ${KIND_CONTEXT_NAME} 2>/dev/null || kubectl cluster-info --context kind-${KIND_CLUSTER_NAME} 2>/dev/null || echo "Cluster not running."

# Install Tilt if not present in _output/bin or wrong version
dev-install-tilt:
	@if [ ! -f ${TILT_BIN} ] || [ "$$(${TILT_BIN} version | grep -Eo '[0-9]+\.[0-9]+\.[0-9]+' | head -1)" != "${TILT_VERSION}" ]; then \
		mkdir -p ${BIN_DIR}; \
		TMP_DIR=$$(mktemp -d); \
		curl -fsSL https://github.com/tilt-dev/tilt/releases/download/v${TILT_VERSION}/tilt.${TILT_VERSION}.linux.${HOSTARCH}.tar.gz | tar -xz -C $$TMP_DIR && \
		mv $$TMP_DIR/tilt ${TILT_BIN}; \
		rm -rf $$TMP_DIR; \
		chmod +x ${TILT_BIN}; \
		printf "Tilt installed at ${TILT_BIN} (version ${TILT_VERSION}).\n"; \
	else \
		printf "Tilt already installed at ${TILT_BIN} (version ${TILT_VERSION}).\n"; \
	fi

.PHONY: dev-tilt-status
dev-tilt-status: dev-install-tilt
	${TILT_BIN} get uiresource

.PHONY: dev-tilt-logs
dev-tilt-logs: dev-install-tilt
	@if [ -n "${RESOURCE}" ]; then \
		${TILT_BIN} logs ${RESOURCE}; \
	else \
		${TILT_BIN} logs; \
	fi

.PHONY: dev-tilt-describe
dev-tilt-describe: dev-install-tilt
	@if [ -z "${RESOURCE}" ]; then \
		echo "RESOURCE is required. Example: make dev-tilt-describe RESOURCE=volcano-scheduler"; \
		exit 1; \
	fi
	${TILT_BIN} describe uiresource ${RESOURCE}

.PHONY: dev-tilt-trigger
dev-tilt-trigger: dev-install-tilt
	@if [ -z "${RESOURCE}" ]; then \
		echo "RESOURCE is required. Example: make dev-tilt-trigger RESOURCE=volcano-scheduler"; \
		exit 1; \
	fi
	${TILT_BIN} trigger ${RESOURCE}

.PHONY: dev-wait-ready
dev-wait-ready: dev-install-tilt
	${TILT_BIN} wait --for=condition=Ready ${TILT_WAIT_RESOURCES} --timeout=${TILT_WAIT_TIMEOUT}
