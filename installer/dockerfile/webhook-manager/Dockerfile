# Copyright 2019 The Volcano Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM golang:1.20.1 AS builder
WORKDIR /go/src/volcano.sh/
COPY go.mod go.sum ./
RUN go mod download
ADD . volcano
RUN cd volcano && make vc-webhook-manager

FROM alpine:latest
ARG KUBE_VERSION="1.29.0"
ARG TARGETARCH
ARG APK_MIRROR
RUN if [[ -n "$APK_MIRROR" ]]; then sed -i "s@https://dl-cdn.alpinelinux.org@${APK_MIRROR}@g" /etc/apk/repositories ; fi && \
    apk add --update ca-certificates && \
    apk add --update openssl && \
    apk add --update -t deps curl && \
    curl -L https://storage.googleapis.com/kubernetes-release/release/v$KUBE_VERSION/bin/linux/$TARGETARCH/kubectl -o /usr/local/bin/kubectl && \
    chmod +x /usr/local/bin/kubectl && \
    apk del --purge deps && \
    rm /var/cache/apk/*

COPY --from=builder /go/src/volcano.sh/volcano/_output/bin/vc-webhook-manager /vc-webhook-manager
ADD ./installer/dockerfile/webhook-manager/gen-admission-secret.sh /gen-admission-secret.sh
ENTRYPOINT ["/vc-webhook-manager"]
