BIN_DIR=_output/bin
CHART_DIR=_output/chart
IMAGE_DIR=_output/image
IMAGE=volcano
TAG=0.1
VERSION?=${TAG}
CHART_VERSION?=${VERSION}

.EXPORT_ALL_VARIABLES:

all: controllers scheduler cli admission

init:
	mkdir -p ${BIN_DIR}
	mkdir -p ${CHART_DIR}
	mkdir -p ${IMAGE_DIR}

controllers:
	go build -o ${BIN_DIR}/vk-controllers ./cmd/controllers

scheduler:
	go build -o ${BIN_DIR}/vk-scheduler ./cmd/scheduler

cli:
	go build -o ${BIN_DIR}/vkctl ./cmd/cli

admission:
	go build -o ${BIN_DIR}/vk-admission ./cmd/admission

release:
	CGO_ENABLED=0 go build -o ${BIN_DIR}/rel/vk-controllers ./cmd/controllers
	CGO_ENABLED=0 go build -o ${BIN_DIR}/rel/vk-scheduler ./cmd/scheduler
	CGO_ENABLED=0 go build -o  ${BIN_DIR}/rel/vk-admission ./cmd/admission

docker: release
	for name in controllers scheduler admission; do\
		cp ${BIN_DIR}/rel/vk-$$name ./installer/dockerfile/$$name/; \
		docker build --no-cache -t $(IMAGE)-$$name:$(TAG) ./installer/dockerfile/$$name; \
		rm installer/dockerfile/$$name/vk-$$name; \
	done

generate-code:
	./hack/update-gencode.sh

e2e-test:
	./hack/run-e2e.sh

unit-test:
	go list ./... | grep -v e2e | xargs go test -v

e2e-test-kind:
	./hack/run-e2e-kind.sh

clean:
	rm -rf _output/
	rm -f *.log

verify: generate-code
	hack/verify-gofmt.sh
	hack/verify-golint.sh
	hack/verify-gencode.sh

chart: init
	helm package installer/chart/volcano --version=${CHART_VERSION} --destination=${CHART_DIR}

package: docker chart cli
	for name in controllers scheduler admission; do \
		docker save $(IMAGE)-$$name:$(TAG) > ${IMAGE_DIR}/$(IMAGE)-$$name.$(TAG).tar; \
	done
	gzip ${IMAGE_DIR}/*.tar
	tar -zcvf _output/Volcano-package-${VERSION}.tgz -C _output/ ./bin/vkctl ./chart ./image
