# Copyright 2019 The Volcano Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

BIN_DIR=_output/bin
RELEASE_DIR=_output/release
REPO_PATH=volcano.sh/volcano
IMAGE_PREFIX=volcanosh
CRD_OPTIONS ?= "crd:crdVersions=v1,generateEmbeddedObjectMeta=true"
CRD_OPTIONS_EXCLUDE_DESCRIPTION=${CRD_OPTIONS}",maxDescLen=0"
CC ?= "gcc"
SUPPORT_PLUGINS ?= "no"
CRD_VERSION ?= v1
BUILDX_OUTPUT_TYPE ?= "docker"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

OS=$(shell uname -s)

# Get OS architecture
OSARCH=$(shell uname -m)
ifeq ($(OSARCH),x86_64)
GOARCH?=amd64
else ifeq ($(OSARCH),x64)
GOARCH?=amd64
else ifeq ($(OSARCH),aarch64)
GOARCH?=arm64
else ifeq ($(OSARCH),aarch64_be)
GOARCH?=arm64
else ifeq ($(OSARCH),armv8b)
GOARCH?=arm64
else ifeq ($(OSARCH),armv8l)
GOARCH?=arm64
else ifeq ($(OSARCH),i386)
GOARCH?=x86
else ifeq ($(OSARCH),i686)
GOARCH?=x86
else ifeq ($(OSARCH),arm)
GOARCH?=arm
else
GOARCH?=$(OSARCH)
endif

# Run `make images DOCKER_PLATFORMS="linux/amd64,linux/arm64" BUILDX_OUTPUT_TYPE=registry IMAGE_PREFIX=[yourregistry]` to push multi-platform
DOCKER_PLATFORMS ?= "linux/${GOARCH}"

GOOS ?= linux

include Makefile.def

.EXPORT_ALL_VARIABLES:

all: vc-scheduler vc-controller-manager vc-webhook-manager vcctl command-lines

init:
	mkdir -p ${BIN_DIR}
	mkdir -p ${RELEASE_DIR}

vc-scheduler: init
	if [ ${SUPPORT_PLUGINS} = "yes" ];then\
		CC=${CC} CGO_ENABLED=1 go build -ldflags ${LD_FLAGS} -o ${BIN_DIR}/vc-scheduler ./cmd/scheduler;\
	else\
		CC=${CC} CGO_ENABLED=0 go build -ldflags ${LD_FLAGS} -o ${BIN_DIR}/vc-scheduler ./cmd/scheduler;\
	fi;

vc-controller-manager: init
	CC=${CC} CGO_ENABLED=0 go build -ldflags ${LD_FLAGS} -o ${BIN_DIR}/vc-controller-manager ./cmd/controller-manager

vc-webhook-manager: init
	CC=${CC} CGO_ENABLED=0 go build -ldflags ${LD_FLAGS} -o ${BIN_DIR}/vc-webhook-manager ./cmd/webhook-manager

vcctl: init
	if [ ${OS} = 'Darwin' ];then\
		CC=${CC} CGO_ENABLED=0 GOOS=darwin go build -ldflags ${LD_FLAGS} -o ${BIN_DIR}/vcctl ./cmd/cli;\
	else\
		CC=${CC} CGO_ENABLED=0 go build -ldflags ${LD_FLAGS} -o ${BIN_DIR}/vcctl ./cmd/cli;\
	fi;

image_bins: vc-scheduler vc-controller-manager vc-webhook-manager

images:
	for name in controller-manager scheduler webhook-manager; do\
		docker buildx build -t "${IMAGE_PREFIX}/vc-$$name:$(TAG)" . -f ./installer/dockerfile/$$name/Dockerfile --output=type=${BUILDX_OUTPUT_TYPE} --platform ${DOCKER_PLATFORMS} --build-arg APK_MIRROR=${APK_MIRROR}; \
	done

generate-code:
	./hack/update-gencode.sh

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	go mod vendor
	# volcano crd base
	$(CONTROLLER_GEN) $(CRD_OPTIONS) paths="./vendor/volcano.sh/apis/pkg/apis/scheduling/v1beta1;./vendor/volcano.sh/apis/pkg/apis/batch/v1alpha1;./vendor/volcano.sh/apis/pkg/apis/bus/v1alpha1;./vendor/volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1" output:crd:artifacts:config=config/crd/volcano/bases
	# generate volcano job crd yaml without description to avoid yaml size limit when using `kubectl apply`
	$(CONTROLLER_GEN) $(CRD_OPTIONS_EXCLUDE_DESCRIPTION) paths="./vendor/volcano.sh/apis/pkg/apis/batch/v1alpha1" output:crd:artifacts:config=config/crd/volcano/bases
	# volcano crd v1beta1
	$(CONTROLLER_GEN) "crd:crdVersions=v1beta1" paths="./vendor/volcano.sh/apis/pkg/apis/scheduling/v1beta1;./vendor/volcano.sh/apis/pkg/apis/batch/v1alpha1;./vendor/volcano.sh/apis/pkg/apis/bus/v1alpha1;./vendor/volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1" output:crd:artifacts:config=config/crd/volcano/v1beta1
	# generate volcano job crd yaml without description to avoid yaml size limit when using `kubectl apply`
	$(CONTROLLER_GEN) "crd:maxDescLen=0,crdVersions=v1beta1" paths="./vendor/volcano.sh/apis/pkg/apis/batch/v1alpha1" output:crd:artifacts:config=config/crd/volcano/v1beta1
	# jobflow crd base
	$(CONTROLLER_GEN) $(CRD_OPTIONS) paths="./vendor/volcano.sh/apis/pkg/apis/flow/v1alpha1" output:crd:artifacts:config=config/crd/jobflow/bases

unit-test:
	go clean -testcache
	if [ ${OS} = 'Darwin' ];then\
		GOOS=darwin go list ./... | grep -v "/e2e" | xargs  go test;\
	else\
		go test -p 8 -race $$(find pkg cmd -type f -name '*_test.go' | sed -r 's|/[^/]+$$||' | sort | uniq | sed "s|^|volcano.sh/volcano/|");\
	fi;

e2e: images
	./hack/run-e2e-kind.sh

e2e-test-schedulingbase: images
	E2E_TYPE=SCHEDULINGBASE ./hack/run-e2e-kind.sh

e2e-test-schedulingaction: images
	E2E_TYPE=SCHEDULINGACTION ./hack/run-e2e-kind.sh

e2e-test-jobp: images
	E2E_TYPE=JOBP ./hack/run-e2e-kind.sh

e2e-test-jobseq: images
	E2E_TYPE=JOBSEQ ./hack/run-e2e-kind.sh

e2e-test-vcctl: vcctl images
	E2E_TYPE=VCCTL ./hack/run-e2e-kind.sh

e2e-test-stress: images
	E2E_TYPE=STRESS ./hack/run-e2e-kind.sh

generate-yaml: init manifests
	./hack/generate-yaml.sh TAG=${RELEASE_VER} CRD_VERSION=${CRD_VERSION}

generate-charts: init manifests
	./hack/generate-charts.sh 
	
release-env:
	./hack/build-env.sh release

dev-env:
	./hack/build-env.sh dev

release: images generate-yaml
	./hack/publish.sh

clean:
	rm -rf _output/
	rm -f *.log

verify:
	hack/verify-gofmt.sh
	hack/verify-gencode.sh
    # this verify is deprecated and use make lint-licenses instead.
	#hack/verify-vendor-licenses.sh

lint: ## Lint the files
	hack/verify-golangci-lint.sh

verify-generated-yaml:
	./hack/check-generated-yaml.sh

command-lines:
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vcancel ./cmd/cli/vcancel
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vresume ./cmd/cli/vresume
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vsuspend ./cmd/cli/vsuspend
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vjobs ./cmd/cli/vjobs
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vqueues ./cmd/cli/vqueues
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vsub ./cmd/cli/vsub

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.6.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

update-development-yaml:
	make generate-yaml TAG=latest RELEASE_DIR=installer
	mv installer/volcano-latest.yaml installer/volcano-development.yaml

mod-download-go:
	@-GOFLAGS="-mod=readonly" find -name go.mod -execdir go mod download \;
# go mod tidy is needed with Golang 1.16+ as go mod download affects go.sum
# https://github.com/golang/go/issues/43994
# exclude docs folder
	@find . -path ./docs -prune -o -name go.mod -execdir go mod tidy \;

.PHONY: mirror-licenses
mirror-licenses: mod-download-go; \
	go install istio.io/tools/cmd/license-lint@1.19.7; \
	cd licenses; \
	rm -rf `ls ./ | grep -v LICENSE`; \
	cd -; \
	license-lint --mirror

.PHONY: lint-licenses
lint-licenses:
	@if test -d licenses; then license-lint --config config/license-lint.yaml; fi

.PHONY: licenses-check
licenses-check: mirror-licenses; \
    hack/licenses-check.sh
