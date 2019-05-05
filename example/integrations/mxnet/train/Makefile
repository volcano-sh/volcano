IMG := volcano.sh/volcano/mxnet-train-mnist

TAG := $(shell date +v%Y%m%d)

.PHONY: build

build:
	docker build -t $(IMG):$(TAG) .
	@echo Built $(IMG):$(TAG)
