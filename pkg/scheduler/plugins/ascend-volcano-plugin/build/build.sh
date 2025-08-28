#!/bin/bash
# Perform  build volcano-huawei-npu-scheduler plugin
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

# BASE_VER only support v1.7.0 or v1.9.0
if [ ! -n "$1" ]; then
    BASE_VER=v1.7.0
else
    BASE_VER=$1
fi

echo "Build Version is ${BASE_VER}"

DEFAULT_VER='v6.0.0'
TOP_DIR=${GOPATH}/src/volcano.sh/volcano/
BASE_PATH=${GOPATH}/src/volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/
CMD_PATH=${GOPATH}/src/volcano.sh/volcano/cmd/
PKG_PATH=volcano.sh/volcano/pkg
DATE=$(date "+%Y-%m-%d %H:%M:%S")

function parse_version() {
    version_file="${TOP_DIR}"/service_config.ini
    if  [ -f "$version_file" ]; then
      line=$(sed -n '1p' "$version_file" 2>&1)
      version="v"${line#*=}
      echo "${version}"
      return
    fi
    echo ${DEFAULT_VER}
}

function parse_arch() {
   arch=$(arch 2>&1)
   echo "${arch}"
}

REL_VERSION=$(parse_version)
REL_ARCH=$(parse_arch)
REL_NPU_PLUGIN=volcano-npu_${REL_VERSION}_linux-${REL_ARCH}

function clean() {
    rm -f "${BASE_PATH}"/output/vc-controller-manager
    rm -f "${BASE_PATH}"/output/vc-scheduler
    rm -f "${BASE_PATH}"/output/*.so
}

function copy_yaml() {
    cp "${BASE_PATH}"/build/volcano-"${BASE_VER}".yaml "${BASE_PATH}"/output/
}

# fix the unconditional retry. All pod errors cause the podgroup to be deleted and cannot be rescheduled
function replace_code() {
    REPLACE_FILE="${GOPATH}/src/volcano.sh/volcano/pkg/controllers/job/state/running.go"
    SEARCH_STRING="Ignore"
    if ! grep -q "$SEARCH_STRING" "$REPLACE_FILE";then
      sed -i "s/switch action {/switch action { case \"Ignore\" : return nil/g" "$REPLACE_FILE"
    fi
}

function build() {
    echo "Build Architecture is" "${REL_ARCH}"

    export GO111MODULE=on
    export PATH=$GOPATH/bin:$PATH

    cd "${TOP_DIR}"
    go mod tidy

    cd "${BASE_PATH}"/output/

    export CGO_CFLAGS="-fstack-protector-all -D_FORTIFY_SOURCE=2 -O2 -fPIC -ftrapv"
    export CGO_CPPFLAGS="-fstack-protector-all -D_FORTIFY_SOURCE=2 -O2 -fPIC -ftrapv"
    export CC=/usr/local/musl/bin/musl-gcc
    export CGO_ENABLED=0

    go build -mod=mod -buildmode=pie -ldflags "-s -linkmode=external -extldflags=-Wl,-z,now
      -X '${PKG_PATH}/version.Built=${DATE}' -X '${PKG_PATH}/version.Version=${BASE_VER}'" \
      -o vc-controller-manager "${CMD_PATH}"/controller-manager

    export CGO_ENABLED=1
    go build -mod=mod -buildmode=pie -ldflags "-s -linkmode=external -extldflags=-Wl,-z,now
      -X '${PKG_PATH}/version.Built=${DATE}' -X '${PKG_PATH}/version.Version=${BASE_VER}'" \
      -o vc-scheduler "${CMD_PATH}"/scheduler

    go build -mod=mod -buildmode=plugin -ldflags "-s -linkmode=external -extldflags=-Wl,-z,now
      -X volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin.PluginName=${REL_NPU_PLUGIN}" \
      -o "${REL_NPU_PLUGIN}".so "${GOPATH}"/src/volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/

    if [ ! -f "${BASE_PATH}/output/${REL_NPU_PLUGIN}.so" ]
    then
      echo "fail to find volcano-npu_${REL_VERSION}.so"
      exit 1
    fi

    sed -i "s/name: volcano-npu_.*/name: ${REL_NPU_PLUGIN}/" "${BASE_PATH}"/output/volcano-*.yaml

    chmod 400 "${BASE_PATH}"/output/*.so
    chmod 500 vc-controller-manager vc-scheduler
    chmod 400 "${BASE_PATH}"/output/Dockerfile*
    chmod 400 "${BASE_PATH}"/output/volcano-*.yaml
}

function replace_node_predicate() {
    if [[ "$BASE_VER" == "v1.7.0" ]];then
      return
    fi
    cd $BASE_PATH
    find . -type f ! -path './.git*/*' ! -path './doc/*' -exec sed -i 's/k8s.io\/klog\"/k8s.io\/klog\/v2\"/g' {} +
    REPLACE_FILE="${GOPATH}/src/volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/huawei_npu.go"
    sed -i "s/api.NodeInfo) error {/api.NodeInfo) (\[\]\*api.Status, error) {/g" "$REPLACE_FILE"
    sed -i "s/return predicateErr/return \[\]\*api.Status{}, predicateErr/g" "$REPLACE_FILE"
}

function replace_node_score() {
    REPLACE_FILE="${GOPATH}/src/volcano.sh/volcano/pkg/scheduler/actions/allocate/allocate.go"
    if [[ "$BASE_VER" == "v1.7.0" ]];then
          sed -i '
          /case len(candidateNodes) == 1:/ {
              N
              N
              s/case len(candidateNodes) == 1:.*\n.*\n.*/            default:/
          }' "$REPLACE_FILE"
      return
    fi
    if [[ "$BASE_VER" == "v1.9.0" ]];then
          sed -i '
          /case len(nodes) == 1:/ {
              N
              N
              s/case len(nodes) == 1:.*\n.*\n.*/            default:/
          }' "$REPLACE_FILE"
       return
    fi
    echo "volcano version is $BASE_VER, will not change allocate.go codes"
}

function replace_k8s_version() {
    REPLACE_FILE="${GOPATH}/src/volcano.sh/volcano/go.mod"
    if [[ "$BASE_VER" == "v1.7.0" ]];then
      sed -i "s/1.25.0/1.25.14/g" "$REPLACE_FILE"
      return
    fi
    echo "volcano version is $BASE_VER, will not change go.mod codes"
}

function main() {
  clean
  copy_yaml
  replace_code
  replace_node_predicate
  replace_node_score
  replace_k8s_version
  build
}

main "${1}"

echo ""
echo "Finished!"
echo ""