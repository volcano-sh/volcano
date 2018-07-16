BIN_DIR=_output/bin
RELEASE_VER=0.1

kube-arbitrator: init
	go build -o ${BIN_DIR}/kar-scheduler ./cmd/kar-scheduler/
	go build -o ${BIN_DIR}/kar-controllers ./cmd/kar-controllers/
	go build -o ${BIN_DIR}/karcli ./cmd/karcli

verify: generate-code
	hack/verify-gofmt.sh
	hack/verify-golint.sh
	hack/verify-gencode.sh

init:
	mkdir -p ${BIN_DIR}

generate-code:
	go build -o ${BIN_DIR}/deepcopy-gen ./cmd/deepcopy-gen/
	${BIN_DIR}/deepcopy-gen -i ./pkg/apis/v1alpha1/ -O zz_generated.deepcopy

images: kube-arbitrator
	cp ./_output/bin/kar-scheduler ./deployment/
	cp ./_output/bin/kar-controllers ./deployment/
	cp ./_output/bin/karcli ./deployment/
	docker build ./deployment/ -f ./deployment/Dockerfile.sched -t kubearbitrator/kar-scheduler:${RELEASE_VER}
	docker build ./deployment/ -f ./deployment/Dockerfile.ctrl -t kubearbitrator/kar-controllers:${RELEASE_VER}
	rm -f ./deployment/kar*

run-test:
	hack/make-rules/test.sh $(WHAT) $(TESTS)

coverage:
	KUBE_COVER=y hack/make-rules/test.sh $(WHAT) $(TESTS)

clean:
	rm -rf _output/
	rm -f kube-arbitrator
