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

generate-code:
	go build -o ${BIN_DIR}/deepcopy-gen ./cmd/deepcopy-gen/
	${BIN_DIR}/deepcopy-gen -i ./pkg/apis/batch/v1alpha1/ -O zz_generated.deepcopy

e2e-test:
	./hack/run-e2e.sh

clean:
	rm -rf _output/
