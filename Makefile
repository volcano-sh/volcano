BIN_DIR=_output/bin

all: controllers scheduler cli

init:
	mkdir -p ${BIN_DIR}

controllers:
	go build -o ${BIN_DIR}/vn-controllers ./cmd/controllers

scheduler:
	go build -o ${BIN_DIR}/vn-scheduler ./cmd/scheduler

cli:
	go build -o ${BIN_DIR}/vnctl ./cmd/cli

generate-code:
	go build -o ${BIN_DIR}/deepcopy-gen ./cmd/deepcopy-gen/
	${BIN_DIR}/deepcopy-gen -i ./pkg/apis/core/v1alpha1/ -O zz_generated.deepcopy

clean:
	rm -rf _output/
