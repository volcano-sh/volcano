BIN_DIR=_output/bin

all: controllers scheduler cli

init:
	mkdir -p ${BIN_DIR}

controllers:
	go build -o ${BIN_DIR}/vk-controllers ./cmd/controllers

scheduler:
	go build -o ${BIN_DIR}/vk-scheduler ./cmd/scheduler

cli:
	go build -o ${BIN_DIR}/vkctl ./cmd/cli

webhook:
	CGO_ENABLED=0 gox -os="linux" -output ./cmd/webhook/webhook ./cmd/webhook
	docker build --no-cache -t webhook-server:1.0 ./cmd/webhook

generate-code:
	go build -o ${BIN_DIR}/deepcopy-gen ./cmd/deepcopy-gen/
	${BIN_DIR}/deepcopy-gen -i ./pkg/apis/batch/v1alpha1/ -O zz_generated.deepcopy
	${BIN_DIR}/deepcopy-gen -i ./pkg/apis/bus/v1alpha1/ -O zz_generated.deepcopy

e2e-test:
	./hack/run-e2e.sh

clean:
	rm -rf _output/
