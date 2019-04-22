BIN_DIR=_output/bin
CHART_DIR=_output/chart
IMAGE_DIR=_output/image
IMAGE=volcano
REL_OSARCH="linux/amd64"
TAG=v0.4.2
VERSION?=${TAG}
RELEASE_VER?=${TAG}
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

init:
	mkdir -p ${BIN_DIR}
	mkdir -p ${CHART_DIR}
	mkdir -p ${IMAGE_DIR}

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

docker: images

generate-code:
	./hack/update-gencode.sh

e2e-test:
	./hack/run-e2e.sh

unit-test:
	go list ./... | grep -v e2e | xargs go test -v

e2e-test-kind:
	./hack/run-e2e-kind.sh

clean:
	rm -rf _output/
	rm -f *.log

verify: generate-code
	hack/verify-gofmt.sh
	hack/verify-golint.sh
	hack/verify-gencode.sh

chart: init
	helm package ./deployment/volcano --version=${VERSION} --destination=${CHART_DIR}

package: clean images chart vkctl
	docker save kubesigs/kube-batch:${RELEASE_VER} > ${IMAGE_DIR}/kube-batch.$(RELEASE_VER).tar;
	for name in controllers admission; do \
		docker save $(IMAGE_PREFIX)-$$name:$(RELEASE_VER) > ${IMAGE_DIR}/$(IMAGE)-$$name.$(RELEASE_VER).tar; \
	done
	gzip ${IMAGE_DIR}/*.tar
	tar -zcvf _output/Volcano-package-${VERSION}.tgz -C _output/ ./bin/vkctl ./chart ./image
