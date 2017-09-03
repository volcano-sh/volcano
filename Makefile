BIN_DIR=_output/bin

kube-arbitrator: init
	go build -o ${BIN_DIR}/kube-arbitrator cmd/main.go

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
