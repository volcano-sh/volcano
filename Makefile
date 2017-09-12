BIN_DIR=_output/bin

kube-arbitrator: init
	go build -o ${BIN_DIR}/deepcopy-gen ./cmd/deepcopy-gen/
	${BIN_DIR}/deepcopy-gen -i ./pkg/apis/v1/
	go build -o ${BIN_DIR}/kube-arbitrator ./cmd/kube-arbitrator/

verify:
	hack/verify-gofmt.sh

init:
	mkdir -p ${BIN_DIR}

test-integration:
	hack/make-rules/test-integration.sh $(WHAT)

run-test:
	hack/make-rules/test.sh $(WHAT) $(TESTS)

clean:
	rm -rf _output/
	rm -f kube-arbitrator
