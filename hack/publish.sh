#!/bin/bash

# Copyright 2019 The Volcano Authors.

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail



# The process of preparing volcano release.
#   1. cp binaries into release folder
#   2. cp README document into release folder
#   3. cp default queue into release folder
#   4. cp helm charts template into release folder

VK_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
BINARY_FOLDER=${VK_ROOT}/${BIN_DIR}/${REL_OSARCH}
RELEASE_FOLDER=${VK_ROOT}/${RELEASE_DIR}
RELEASE_BINARY=${RELEASE_FOLDER}/bin
QUEUE_FILE=${VK_ROOT}/installer/helm/chart/volcano/templates/default-queue.yaml
README_FILE=${VK_ROOT}/installer/README.md
HELM_FOLDER=${VK_ROOT}/installer/helm

if [[ ! -f ${RELEASE_BINARY} ]];then
    mkdir ${RELEASE_BINARY}
fi

cp -r ${BINARY_FOLDER} ${RELEASE_BINARY}

cp ${README_FILE} ${RELEASE_FOLDER}

cp ${QUEUE_FILE} ${RELEASE_FOLDER}

cp -r ${HELM_FOLDER} ${RELEASE_FOLDER}


