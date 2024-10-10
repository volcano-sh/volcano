# Copyright 2024 The Volcano Authors.
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

ARG OPEN_EULER_IMAGE_TAG
ARG BWM_RPM_NAME

FROM golang:1.22.2 AS builder
WORKDIR /go/src/volcano.sh/
COPY go.mod go.sum ./
RUN go mod download
ADD . volcano
RUN cd volcano && make vc-agent

FROM openeuler/openeuler:${OPEN_EULER_IMAGE_TAG} AS repo
WORKDIR /
RUN yum install -y cpio && \
    yum install -y --downloadonly --destdir=./ oncn-bwm && \
    rpm2cpio $(ls | grep oncn-bwm) | cpio -div

FROM alpine:latest
RUN apk add sudo
COPY --from=builder /go/src/volcano.sh/volcano/_output/bin/vc-agent /vc-agent
COPY --from=builder /go/src/volcano.sh/volcano/_output/bin/network-qos \
                    /go/src/volcano.sh/volcano/installer/build/volcano-agent/install.sh /usr/local/bin/
COPY --from=repo /usr/share/bwmcli/bwm_tc.o /usr/local/bin/
RUN chmod +x /usr/local/bin/install.sh

