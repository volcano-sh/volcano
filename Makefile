BIN_DIR=_output/bin

kube-arbitrator: init
	go build -o ${BIN_DIR}/kube-batchd ./cmd/kube-batchd/
	go build -o ${BIN_DIR}/kube-queue-ctrl ./cmd/kube-queue-ctrl/
	go build -o ${BIN_DIR}/kube-queuejob-ctrl ./cmd/kube-queuejob-ctrl/
	go build -o ${BIN_DIR}/kube-quotalloc ./cmd/kube-quotalloc/

verify: generate-code
	hack/verify-gofmt.sh
	hack/verify-golint.sh
	hack/verify-gencode.sh

init:
	mkdir -p ${BIN_DIR}

generate-code:
	go build -o ${BIN_DIR}/deepcopy-gen ./cmd/deepcopy-gen/
	${BIN_DIR}/deepcopy-gen -i ./pkg/batchd/apis/v1/ -O zz_generated.deepcopy
	${BIN_DIR}/deepcopy-gen -i ./pkg/queue-ctrl/apis/v1/ -O zz_generated.deepcopy
	${BIN_DIR}/deepcopy-gen -i ./pkg/queuejob-ctrl/apis/v1/ -O zz_generated.deepcopy
	${BIN_DIR}/deepcopy-gen -i ./pkg/quotalloc/apis/v1/ -O zz_generated.deepcopy

images: kube-arbitrator
	cp ./_output/bin/kube-batchd ./deployment/
	cp ./_output/bin/kube-queue-ctrl ./deployment/
	cp ./_output/bin/kube-queuejob-ctrl ./deployment/
	docker build ./deployment/ -t kubearbitrator/batchd:v0.1
	docker build ./deployment/ -t kubearbitrator/queue-ctrl:v0.1
	docker build ./deployment/ -t kubearbitrator/queuejob-ctrl:v0.1
	rm -f ./deployment/kube-batchd
	rm -f ./deployment/kube-queuejob-ctrl

test:
	hack/make-rules/test.sh $(WHAT) $(TESTS)

clean:
	rm -rf _output/
	rm -f kube-arbitrator
