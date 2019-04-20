BIN_DIR=_output/bin
RELEASE_VER=v0.4.2
REPO_PATH=github.com/kubernetes-sigs/kube-batch
GitSHA=`git rev-parse HEAD`
Date=`date "+%Y-%m-%d %H:%M:%S"`
REL_OSARCH="linux/amd64"
IMAGE_PREFIX=kubesigs/vk
LD_FLAGS=" \
    -X '${REPO_PATH}/pkg/version.GitSHA=${GitSHA}' \
    -X '${REPO_PATH}/pkg/version.Built=${Date}'   \
    -X '${REPO_PATH}/pkg/version.Version=${RELEASE_VER}'"

.EXPORT_ALL_VARIABLES:

all: kube-batch vk-controllers vk-admission vkctl

kube-batch: init
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/kube-batch ./cmd/kube-batch

vk-controllers: init
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vk-controllers ./cmd/controllers

vk-admission: init
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vk-admission ./cmd/admission

vkctl: init
	go build -ldflags ${LD_FLAGS} -o=${BIN_DIR}/vkctl ./cmd/cli

verify: generate-code
	hack/verify-gofmt.sh
	hack/verify-golint.sh
	hack/verify-gencode.sh
	hack/verify-boilerplate.sh
	hack/verify-spelling.sh

init:
	mkdir -p ${BIN_DIR}

rel_bins:
	go get github.com/mitchellh/gox
	#Build kube-batch binary
	CGO_ENABLED=0 gox -osarch=${REL_OSARCH} -ldflags ${LD_FLAGS} \
	-output=${BIN_DIR}/{{.OS}}/{{.Arch}}/kube-batch ./cmd/kube-batch
	#Build job controller & job admission
	#TODO: Add version support in job controller and admission to make LD_FLAGS work
	for name in controllers admission; do\
		CGO_ENABLED=0 gox -osarch=${REL_OSARCH} -ldflags ${LD_FLAGS} -output ${BIN_DIR}/{{.OS}}/{{.Arch}}/vk-$$name ./cmd/$$name; \
	done

images: rel_bins
	#Build kube-batch images
	cp ${BIN_DIR}/${REL_OSARCH}/kube-batch ./deployment/images/
	docker build ./deployment/images -t kubesigs/kube-batch:${RELEASE_VER}
	rm -f ./deployment/images/kube-batch
	#Build job controller and admission images
	for name in controllers admission; do\
		cp ${BIN_DIR}/${REL_OSARCH}/vk-$$name ./deployment/images/$$name/; \
		docker build --no-cache -t $(IMAGE_PREFIX)-$$name:$(RELEASE_VER) ./deployment/images/$$name; \
		rm deployment/images/$$name/vk-$$name; \
    done

run-test:
	hack/make-rules/test.sh $(WHAT) $(TESTS)

e2e: kube-batch
	hack/run-e2e.sh

generate-code:
	./hack/update-gencode.sh

e2e-kind: vkctl images
	./hack/run-e2e-kind.sh

coverage:
	KUBE_COVER=y hack/make-rules/test.sh $(WHAT) $(TESTS)

benchmark:
	#NOTE: !Only GCE platform is supported now
	test/kubemark/start-kubemark.sh
	go test ./test/e2e/kube-batch -v -timeout 30m --ginkgo.focus="Feature:Performance"
	test/kubemark/stop-kubemark.sh

clean:
	rm -rf _output/
	rm -f kube-batch
