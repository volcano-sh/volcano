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
REL_OSARCH=linux/amd64
REPO_PATH=volcano.sh/volcano
IMAGE_PREFIX=volcanosh/vc
# If tag not explicitly set in users default to the git sha.
TAG ?= $(shell git rev-parse --verify HEAD)
RELEASE_VER=v0.1
GitSHA=`git rev-parse HEAD`
Date=`date "+%Y-%m-%d %H:%M:%S"`
LD_FLAGS=" \
    -X '${REPO_PATH}/pkg/version.GitSHA=${GitSHA}' \
    -X '${REPO_PATH}/pkg/version.Built=${Date}'   \
    -X '${REPO_PATH}/pkg/version.Version=${RELEASE_VER}'"

.EXPORT_ALL_VARIABLES:

all: vc-scheduler vc-controllers vc-admission vcctl

init:
	mkdir -p ${BIN_DIR}
	mkdir -p ${RELEASE_DIR}

vc-scheduler: init
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vc-scheduler ./cmd/scheduler

vc-controllers: init
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vc-controllers ./cmd/controllers

vc-admission: init
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vc-admission ./cmd/admission

vcctl: init
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vcctl ./cmd/cli

image_bins: init
	go get github.com/mitchellh/gox
	CGO_ENABLED=0 gox -osarch=${REL_OSARCH} -ldflags ${LD_FLAGS} -output ${BIN_DIR}/${REL_OSARCH}/vcctl ./cmd/cli
	for name in controllers scheduler admission; do\
		CGO_ENABLED=0 gox -osarch=${REL_OSARCH} -ldflags ${LD_FLAGS} -output ${BIN_DIR}/${REL_OSARCH}/vc-$$name ./cmd/$$name; \
	done

images: image_bins
	for name in controllers scheduler admission; do\
		cp ${BIN_DIR}/${REL_OSARCH}/vc-$$name ./installer/dockerfile/$$name/; \
		docker build --no-cache -t $(IMAGE_PREFIX)-$$name:$(TAG) ./installer/dockerfile/$$name; \
		rm installer/dockerfile/$$name/vc-$$name; \
	done

admission-base-image:
	docker build --no-cache -t $(IMAGE_PREFIX)-admission-base:$(TAG) ./installer/dockerfile/admission/ -f ./installer/dockerfile/admission/Dockerfile.base;

generate-code:
	./hack/update-gencode.sh

unit-test:
	go list ./... | grep -v e2e | xargs go test -v -race

e2e-test-kind:
	./hack/run-e2e-kind.sh

generate-yaml: init
	./hack/generate-yaml.sh


release: images generate-yaml
	./hack/publish.sh

clean:
	rm -rf _output/
	rm -f *.log

verify: generate-code
	hack/verify-gofmt.sh
	hack/verify-golint.sh
	hack/verify-gencode.sh

verify-generated-yaml:
	./hack/check-generated-yaml.sh

