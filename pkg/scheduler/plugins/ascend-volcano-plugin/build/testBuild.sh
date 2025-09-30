#!/bin/bash
# Perform  test volcano-huawei-npu-scheduler plugin
# Copyright @ Huawei Technologies CO., Ltd. 2020-2022. All rights reserved
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
# ============================================================================

set -e

export GO111MODULE=on
export GONOSUMDB="*"
export PATH=$GOPATH/bin:$PATH

cd "${GOPATH}"/src/volcano.sh/volcano
go get github.com/agiledragon/gomonkey/v2@v2.3.0
go mod vendor

file_input='testVolcano.txt'
file_detail_output='api.html'

echo "************************************* Start LLT Test *************************************"
mkdir -p "${GOPATH}"/src/volcano.sh/volcano/_output/test/
cd "${GOPATH}"/src/volcano.sh/volcano/_output/test/
rm -f $file_detail_output $file_input

if  ! go test -v -race -gcflags=all=-l -coverprofile cov.out "${GOPATH}"/src/volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/... \
    > ./$file_input;
then
  echo '****** go test cases error! ******'
  echo 'Failed' > $file_input
  exit 1
else
  gocov convert cov.out | gocov-html >"$file_detail_output"
  gotestsum --junitfile unit-tests.xml "${GOPATH}"/src/volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/...

  total_coverage=$(go tool cover -func=cov.out | grep "total:" | awk '{print $3}'| sed 's/%//')
  # round up
  coverage=$(echo "$total_coverage" | awk '{if ($1 >= 0) print ($1 == int($1)) ? int($1) : int($1) + 1;\
                                        else print ($1 == int($1)) ? int($1) : int($1)}')
  if [[ $coverage -ge 80 ]]; then
    echo "coverage passed: $coverage%"
  else
    echo "coverage failed: $coverage%, it needs to be greater than 80%."
    exit 1
  fi
fi

echo "************************************* End   LLT Test *************************************"
exit 0