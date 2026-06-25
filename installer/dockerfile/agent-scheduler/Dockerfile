# Copyright 2025 The Volcano Authors.
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

FROM golang:1.25.0@sha256:5502b0e56fca23feba76dbc5387ba59c593c02ccc2f0f7355871ea9a0852cebe AS builder
WORKDIR /go/src/volcano.sh/
COPY go.mod go.sum ./
# Copy staging directory before go mod download since go.mod has replace directive
COPY staging/ ./staging/
RUN go mod download
ADD . volcano
RUN cd volcano && make vc-agent-scheduler

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
COPY --from=builder /go/src/volcano.sh/volcano/_output/bin/vc-agent-scheduler /vc-agent-scheduler
ENTRYPOINT ["/vc-agent-scheduler"]
