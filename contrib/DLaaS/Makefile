BIN_DIR=_output/bin
RELEASE_VER=v0.2
CONTRIB_VENDOR_GOPATH=${HOME}/tmp/contrib/DLaaS
CURRENT_DIR=$(shell pwd)
CONTRIB_VENDOR_GOPATH_SRC_TARGET=${CURRENT_DIR}/vendor
SYM_LINK_EXISTS=$(shell [ -e ${CONTRIB_VENDOR_GOPATH}/src ] && echo 1 || echo 0 )
ORIG_GOPATH=${GOPATH}

kar-controller: init
	go build -o ${BIN_DIR}/kar-controllers ./cmd/kar-controllers/

verify: generate-code
#	hack/verify-gofmt.sh
#	hack/verify-golint.sh
#	hack/verify-gencode.sh

init:
	mkdir -p ${BIN_DIR}

generate-code: set_gopath_to_generate_code
	GOPATH=${ORIG_GOPATH} go build -o ${BIN_DIR}/deepcopy-gen ./cmd/deepcopy-gen/
	GOPATH=${CONTRIB_VENDOR_GOPATH}:${GOPATH} ${BIN_DIR}/deepcopy-gen -i ./pkg/apis/controller/v1alpha1/ -O zz_generated.deepcopy  -o ../../../../..

images: kube-batch
	cp ./_output/bin/kube-batch ./deployment/images/
	GOPATH=${ORIG_GOPATH} docker build ./deployment/images -t kubesigs/kube-batch:${RELEASE_VER}
	rm -f ./deployment/images/kube-batch

run-test:
#	hack/make-rules/test.sh $(WHAT) $(TESTS)

e2e: 
#	kube-controller
#	hack/run-e2e.sh

coverage:
#	KUBE_COVER=y hack/make-rules/test.sh $(WHAT) $(TESTS)

clean:
	rm -rf _output/
	rm -f kar-controllers 

set_gopath_to_generate_code: set_gopath_to_generate_code_clean
	$(info Make a temporary path to hold the creation of a symbolic link for contrib vendor directory)
	mkdir -p ${CONTRIB_VENDOR_GOPATH}
	$(info Set symbolic link inside the path of the CONTRIB_VENDOR_GOPATH variable to link to vender directory in contrib)
	cd ${CONTRIB_VENDOR_GOPATH} && ln -s ${CONTRIB_VENDOR_GOPATH_SRC_TARGET} src


set_gopath_to_generate_code_clean:
	$(info Removing symbolic link to vender directory in contrib)
	$(shell if [ "${SYM_LINK_EXISTS}" = "1" ]; then cd ${CONTRIB_VENDOR_GOPATH}; rm src; fi;)
