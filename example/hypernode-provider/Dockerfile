FROM golang:1.22.2 AS builder

WORKDIR /go/src/volcano.sh/

# Install musl
RUN apt-get update && \
    apt-get install -y sudo

RUN wget http://musl.libc.org/releases/musl-1.2.1.tar.gz && \
    tar -xf musl-1.2.1.tar.gz && \
    cd musl-1.2.1 && \
    ./configure && make && sudo make install

COPY go.mod go.sum ./

RUN go mod download

ADD . volcano

# Build plugin
RUN cd volcano && CC=/usr/local/musl/bin/musl-gcc CGO_ENABLED=1 \
    go build -buildmode=plugin -ldflags '-linkmode=external' \
    -o example/hypernode-provider/example-provider.so example/hypernode-provider/example_provider.go

# Build vc controller base manager image with plugin enabled
RUN cd volcano && SUPPORT_PLUGINS=yes make vc-controller-manager

# Build vc controller manager image with plugin
FROM alpine:latest
COPY --from=builder /go/src/volcano.sh/volcano/_output/bin/vc-controller-manager /vc-controller-manager
COPY --from=builder /go/src/volcano.sh/volcano/example/hypernode-provider/example-provider.so /
ENTRYPOINT ["/vc-controller-manager"]