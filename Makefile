BIN_DIR=_output/bin
IMAGE = admission-controller-server
TAG = 1.0

all: controllers scheduler cli admission-controller

init:
	mkdir -p ${BIN_DIR}

controllers:
	go build -o ${BIN_DIR}/vk-controllers ./cmd/controllers

scheduler:
	go build -o ${BIN_DIR}/vk-scheduler ./cmd/scheduler

cli:
	go build -o ${BIN_DIR}/vkctl ./cmd/cli

admission-controller:
	go build -o ${BIN_DIR}/ad-controller ./cmd/admission-controller

rel-admission-controller:
	CGO_ENABLED=0 go build -o  ${BIN_DIR}/rel/ad-controller ./cmd/admission-controller

admission-images: rel-admission-controller
	cp ${BIN_DIR}/rel/ad-controller ./cmd/admission-controller/
	docker build --no-cache -t $(IMAGE):$(TAG) ./cmd/admission-controller

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
