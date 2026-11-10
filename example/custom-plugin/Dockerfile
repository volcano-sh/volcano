FROM golang:1.26.2@sha256:b54cbf583d390341599d7bcbc062425c081105cc5ef6d170ced98ef9d047c716 AS builder

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
# Copy staging directory before go mod download since go.mod has replace directive
COPY staging/ ./staging/
RUN go mod download

ADD . volcano

# Build plugin
RUN cd volcano && CC=/usr/local/musl/bin/musl-gcc CGO_ENABLED=1 \
    go build -buildmode=plugin -ldflags '-linkmode=external' \
    -o example/custom-plugin/magic.so example/custom-plugin/magic.go

# Build vc scheduler base image with plugin enabled
RUN cd volcano && SUPPORT_PLUGINS=yes make vc-scheduler

# Build vc scheduler image with plugin
FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
COPY --from=builder /go/src/volcano.sh/volcano/_output/bin/vc-scheduler /vc-scheduler
COPY --from=builder /go/src/volcano.sh/volcano/example/custom-plugin/magic.so /plugins/magic.so
ENTRYPOINT ["/vc-scheduler"]