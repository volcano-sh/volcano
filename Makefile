BIN_DIR=_output/bin

kube-arbitrator: init
	go build -o ${BIN_DIR}/kube-arbitrator cmd/main.go

test:
	echo "unit test script is not ready!"

init:
	mkdir -p ${BIN_DIR}
