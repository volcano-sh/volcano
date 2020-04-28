#!/bin/bash

# Copyright 2014 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Compare version numbers
kube::golang::version_gt() {
    return "$(printf '%s\n' "$@" | sort -V | head -n 1)" != "$1";
}

# Ensure the go tool exists and is a viable version.
kube::golang::verify_go_version() {
  if [[ -z "$(which go)" ]]; then
    kube::log::usage_from_stdin <<EOF
Can't find 'go' in PATH, please fix and retry.
See http://golang.org/doc/install for installation instructions.
EOF
    return 2
  fi

  local go_version
  go_version=($(go version))
  local minimum_go_version
  minimum_go_version=go1.12.1
  if [[ "kube::golang::version_gt "${go_version[2]}" "${minimum_go_version}"" == 1 && "${go_version[2]}" != "devel" ]]; then
    kube::log::usage_from_stdin <<EOF
Detected go version: ${go_version[*]}.
Kubernetes requires ${minimum_go_version} or greater.
Please install ${minimum_go_version} or later.
EOF
    return 2
  fi
}
