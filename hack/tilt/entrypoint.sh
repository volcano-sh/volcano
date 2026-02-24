#!/bin/bash
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

set -e

SRC_DIR=/go/src/volcano.sh/volcano
BIN="${SRC_DIR}/vc-${COMPONENT}"
RELOAD_FILE="/tmp/reload"

child_pid=0

start_child() {
  "$BIN" "$@" &
  child_pid=$!
}

stop_child() {
  if [ $child_pid -ne 0 ]; then
    kill $child_pid
    wait $child_pid 2>/dev/null || true
  fi
}

start_child "$@"

while true; do
  if [ -f "$RELOAD_FILE" ]; then
    echo "[entrypoint] Detected reload trigger, rebuilding and restarting..."
    stop_child
    GOOS=linux GOARCH="$(go env GOARCH)" go build -o "$BIN" ./cmd/${COMPONENT}
    start_child "$@"
    rm -f "$RELOAD_FILE"
  fi
  sleep 1
done
