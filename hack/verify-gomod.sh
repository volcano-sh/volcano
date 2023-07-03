#!/usr/bin/env bash

###
#Copyright 2020 The Volcano Authors.
#
#Licensed under the Apache License, Version 2.0 (the "License");
#you may not use this file except in compliance with the License.
#You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
#Unless required by applicable law or agreed to in writing, software
#distributed under the License is distributed on an "AS IS" BASIS,
#WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#See the License for the specific language governing permissions and
#limitations under the License.
###

set -o errexit
set -o nounset
set -o pipefail

# The root of the build/dist directory
VOLCANO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"

echo "running 'go mod tidy' for repo root"
go mod tidy

function volcano::git::check_status() {
	# check if there's any uncommitted changes on go.mod or go.sum /
	echo $( git status --short 2>/dev/null | grep -E "go.mod|go.sum/" |wc -l)
}

ret=$(volcano::git::check_status)
if [ ${ret} -eq 0 ]; then
	echo "SUCCESS: go.mod Verified."
else
	echo  "FAILED: go.mod stale. Please run the command [go mod tidy] to update go.mod ."
	exit 1
fi
