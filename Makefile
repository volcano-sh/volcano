BIN_DIR=_output/bin

kube-arbitrator: init
	go build -o ${BIN_DIR}/kar-scheduler ./cmd/kar-scheduler/
	go build -o ${BIN_DIR}/karcli ./cmd/karcli

verify: generate-code
	hack/verify-gofmt.sh
	hack/verify-golint.sh
	hack/verify-gencode.sh

init:
	mkdir -p ${BIN_DIR}

generate-code:
	go build -o ${BIN_DIR}/deepcopy-gen ./cmd/deepcopy-gen/
	${BIN_DIR}/deepcopy-gen -i ./pkg/apis/v1/ -O zz_generated.deepcopy

images: kube-arbitrator
	cp ./_output/bin/kube-batchd ./deployment/
	cp ./_output/bin/kube-queuejob-ctrl ./deployment/
	docker build ./deployment/ -t kubearbitrator/batchd:v0.1
	docker build ./deployment/ -t kubearbitrator/queuejob-ctrl:v0.1
	rm -f ./deployment/kube-batchd
	rm -f ./deployment/kube-queuejob-ctrl

test:
	hack/make-rules/test.sh $(WHAT) $(TESTS)

clean:
	rm -rf _output/
	rm -f kube-arbitrator
