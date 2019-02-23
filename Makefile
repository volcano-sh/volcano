BIN_DIR=_output/bin
IMAGE-CONTROLLER = vk-controllers
IMAGE-SCHEDULER = vk-scheduler
IMAGE-ADMISSION = vk-admission
TAG = 1.0

all: controllers scheduler cli admission

init:
	mkdir -p ${BIN_DIR}

controllers:
	go build -o ${BIN_DIR}/vk-controllers ./cmd/controllers

scheduler:
	go build -o ${BIN_DIR}/vk-scheduler ./cmd/scheduler

cli:
	go build -o ${BIN_DIR}/vkctl ./cmd/cli

admission:
	go build -o ${BIN_DIR}/ad-controller ./cmd/admission-controller

release:
	CGO_ENABLED=0 go build -o ${BIN_DIR}/rel/vk-controllers ./cmd/controllers
	CGO_ENABLED=0 go build -o ${BIN_DIR}/rel/vk-scheduler ./cmd/scheduler
	CGO_ENABLED=0 go build -o  ${BIN_DIR}/rel/ad-controller ./cmd/admission-controller

docker: release
	cp ${BIN_DIR}/rel/vk-controllers ./installer/dockerfile/controllers/
	docker build --no-cache -t $(IMAGE-CONTROLLER):$(TAG) ./installer/dockerfile/controllers
	rm installer/dockerfile/controllers/vk-controllers
	cp ${BIN_DIR}/rel/vk-scheduler ./installer/dockerfile/scheduler/
	docker build --no-cache -t $(IMAGE-SCHEDULER):$(TAG) ./installer/dockerfile/scheduler
	rm installer/dockerfile/scheduler/vk-scheduler
	cp ${BIN_DIR}/rel/ad-controller ./installer/dockerfile/admission/
	docker build --no-cache -t $(IMAGE-ADMISSION):$(TAG) ./installer/dockerfile/admission
	rm installer/dockerfile/admission/ad-controller

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
