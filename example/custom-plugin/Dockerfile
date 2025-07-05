FROM golang:1.24.0 AS builder

WORKDIR /go/src/volcano.sh/

# Install musl
RUN apt-get update && \
    apt-get install -y sudo

RUN wget http://musl.libc.org/releases/musl-latest.tar.gz && \
    mkdir musl-latest && \
    tar -xf musl-latest.tar.gz -C musl-latest --strip-components=1 && \
    cd musl-latest && \
    ./configure && make && sudo make install

COPY go.mod go.sum ./

RUN go mod download

ADD . volcano

# Build plugin
RUN cd volcano && CC=/usr/local/musl/bin/musl-gcc CGO_ENABLED=1 \
    go build -buildmode=plugin -ldflags '-linkmode=external' \
    -o example/custom-plugin/magic.so example/custom-plugin/magic.go

# Build vc scheduler base image with plugin enabled
RUN cd volcano && SUPPORT_PLUGINS=yes make vc-scheduler

# Build vc scheduler image with plugin
FROM alpine:latest
COPY --from=builder /go/src/volcano.sh/volcano/_output/bin/vc-scheduler /vc-scheduler
COPY --from=builder /go/src/volcano.sh/volcano/example/custom-plugin/magic.so /plugins/magic.so
ENTRYPOINT ["/vc-scheduler"]