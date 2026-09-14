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

ARG OPEN_EULER_IMAGE_TAG=22.03-lts-sp2@sha256:e8c56d3579a10fedd43c5e123db1143b6dcbe4cb59a6e45d6b0b2cf9b13a0c3b
ARG BWM_RPM_NAME

FROM golang:1.26.2 AS builder
WORKDIR /go/src/volcano.sh/
COPY go.mod go.sum ./
# Copy staging directory before go mod download since go.mod has replace directive
COPY staging/ ./staging/
RUN go mod download
ADD . volcano
RUN cd volcano && make vc-agent

FROM openeuler/openeuler:${OPEN_EULER_IMAGE_TAG} AS repo
WORKDIR /
RUN yum install -y cpio && \
    yum install -y --downloadonly --destdir=./ oncn-bwm && \
    rpm2cpio $(ls | grep oncn-bwm) | cpio -div

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN apk add sudo libcap
COPY --from=builder /go/src/volcano.sh/volcano/_output/bin/vc-agent /vc-agent
COPY --from=builder /go/src/volcano.sh/volcano/_output/bin/network-qos \
                    /go/src/volcano.sh/volcano/installer/build/volcano-agent/install.sh /usr/local/bin/
COPY --from=repo /usr/share/bwmcli/bwm_tc.o /usr/local/bin/
RUN adduser -u 1000 -D appuser
RUN chmod +x /usr/local/bin/install.sh \
    && setcap "cap_dac_override=eip" /vc-agent \
    && setcap "cap_dac_override=eip" /usr/local/bin/network-qos \
    && echo -e '%appuser ALL=(root) NOPASSWD: /bin/cp -f /usr/local/bin/network-qos /opt/cni/bin\n%appuser ALL=(root) NOPASSWD: /bin/cp -f /usr/local/bin/bwm_tc.o /usr/share/bwmcli' >> /etc/sudoers

