BIN_DIR=_output/bin
BIN_OSARCH=linux/amd64
IMAGE=volcanosh/vk
TAG = latest

.EXPORT_ALL_VARIABLES:

all: init verify cli images e2e-test-kind

init:
	mkdir -p ${BIN_DIR}

cli:
	go get github.com/mitchellh/gox
	CGO_ENABLED=0 gox -osarch=${BIN_OSARCH} -output ${BIN_DIR}/${BIN_OSARCH}/vkctl ./cmd/cli

image_bins:
	go get github.com/mitchellh/gox
	for name in controllers scheduler admission; do\
		CGO_ENABLED=0 gox -osarch=${BIN_OSARCH} -output ${BIN_DIR}/${BIN_OSARCH}/vk-$$name ./cmd/$$name; \
	done

images: image_bins
	for name in controllers scheduler admission; do\
		cp ${BIN_DIR}/${BIN_OSARCH}/vk-$$name ./installer/dockerfile/$$name/; \
		docker build --no-cache -t $(IMAGE)-$$name:$(TAG) ./installer/dockerfile/$$name; \
		rm installer/dockerfile/$$name/vk-$$name; \
	done

generate-code:
	./hack/update-gencode.sh

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
