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
#   4. cp helm charts template into release folder and update default image tag
#   5. cp license file into release folder
#   6. generate zip file

VK_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
RELEASE_FOLDER=${VK_ROOT}/${RELEASE_DIR}
README_FILE=${VK_ROOT}/installer/README.md
HELM_FOLDER=${VK_ROOT}/installer/helm
VOLCANO_IMAGE_TAG=${TAG:-"latest"}
LICENSE_FILE=${VK_ROOT}/LICENSE

cp ${README_FILE} ${RELEASE_FOLDER}

cp -r ${HELM_FOLDER} ${RELEASE_FOLDER}

if [[ -f ${LICENSE_FILE} ]];then
    cp ${LICENSE_FILE} ${RELEASE_FOLDER}
fi

# overwrite the tag name into values yaml
sed -i "s/latest/${VOLCANO_IMAGE_TAG}/g" ${RELEASE_FOLDER}/helm/chart/volcano/values.yaml

echo "Generate release tar files"
cd ${RELEASE_FOLDER}/
tar -zcvf volcano-${VOLCANO_IMAGE_TAG}-${OSTYPE}.tar.gz *
