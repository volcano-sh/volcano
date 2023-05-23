#!/bin/bash

#
# Copyright 2023 The Volcano Authors.
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
#

set -o errexit
set -o nounset
set -o pipefail

VK_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
export HELM_BIN_DIR=${VK_ROOT}/${BIN_DIR}
export RELEASE_FOLDER=${VK_ROOT}/${RELEASE_DIR}

export HELM_VER=${HELM_VER:-v3.6.3}
export VOLCANO_CHART_VERSION=${TAG:-"latest"}
export VOLCANO_IMAGE_TAG=${VOLCANO_CHART_VERSION}

LOCAL_OS=${OSTYPE}
case $LOCAL_OS in
  "linux"*)
    LOCAL_OS='linux'
    alias sed_i='sed -i'
    ;;
  "darwin"*)
    LOCAL_OS='darwin'
    alias sed_i='sed -i ""'
    ;;
  *)
    echo "This system's OS ${LOCAL_OS} isn't recognized/supported"
    exit 1
    ;;
esac

# Enable the alias function in the Shell
shopt -s  expand_aliases 
ARCH=$(go env GOARCH)

# Step1. install helm binary
if [[ ! -f "${HELM_BIN_DIR}/version.helm.${HELM_VER}" ]] || [[ ! -f "${HELM_BIN_DIR}/helm" ]] ; then
    TD=$(mktemp -d)
    cd "${TD}" && \
        curl -Lo "${TD}/helm.tgz" "https://get.helm.sh/helm-${HELM_VER}-${LOCAL_OS}-${ARCH}.tar.gz" && \
        tar xfz helm.tgz && \
        mv ${LOCAL_OS}-${ARCH}/helm "${HELM_BIN_DIR}/helm-${HELM_VER}" && \
        cp "${HELM_BIN_DIR}/helm-${HELM_VER}" "${HELM_BIN_DIR}/helm" && \
        chmod +x ${HELM_BIN_DIR}/helm
        rm -rf "${TD}"

        if [[ ! -f "${HELM_BIN_DIR}/version.helm.${HELM_VER}" ]] ; then
          touch "${HELM_BIN_DIR}/version.helm.${HELM_VER}"
        fi
fi

echo "generate chart"
CHART_ORIGIN_PATH=${VK_ROOT}/installer/helm/chart
CHART_OUT_PATH=${RELEASE_FOLDER}/chart
rm -rf ${RELEASE_FOLDER}/chart
cp -r "${CHART_ORIGIN_PATH}" "${CHART_OUT_PATH}"
sed_i "s|image_tag_version: \"latest\"|image_tag_version: \"${VOLCANO_IMAGE_TAG}\"|g" $(find ${CHART_OUT_PATH} -type f | grep values.yaml)
sed_i "s|version: 1.5|version: ${VOLCANO_CHART_VERSION}|g" $(find ${CHART_OUT_PATH} -type f | grep Chart.yaml)
sed_i "s|appVersion: 0.1|appVersion: ${VOLCANO_CHART_VERSION}|g" $(find ${CHART_OUT_PATH} -type f | grep Chart.yaml)
helm package "${CHART_OUT_PATH}/volcano" -d "${CHART_OUT_PATH}"
echo "helm package end, charts is in ${CHART_OUT_PATH}"
