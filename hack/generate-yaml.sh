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

VK_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
export HELM_BIN_DIR=${VK_ROOT}/${BIN_DIR}
export RELEASE_FOLDER=${VK_ROOT}/${RELEASE_DIR}

export HELM_VER=${HELM_VER:-v2.13.0}
export VOLCANO_IMAGE_TAG=${TAG:-"latest"}
export YAML_FILENAME=volcano-${VOLCANO_IMAGE_TAG}.yaml

LOCAL_OS=${OSTYPE}
case $LOCAL_OS in
  "linux"*)
    LOCAL_OS='linux'
    ;;
  "darwin"*)
    LOCAL_OS='darwin'
    ;;
  *)
    echo "This system's OS ${LOCAL_OS} isn't recognized/supported"
    exit 1
    ;;
esac

# Step1. install helm binary
if [[ ! -f "${HELM_BIN_DIR}/version.helm.${HELM_VER}" ]] ; then
    TD=$(mktemp -d)
    cd "${TD}" && \
        curl -Lo "${TD}/helm.tgz" "https://storage.googleapis.com/kubernetes-helm/helm-${HELM_VER}-${LOCAL_OS}-amd64.tar.gz" && \
        tar xfz helm.tgz && \
        mv ${LOCAL_OS}-amd64/helm "${HELM_BIN_DIR}/helm-${HELM_VER}" && \
        cp "${HELM_BIN_DIR}/helm-${HELM_VER}" "${HELM_BIN_DIR}/helm" && \
        chmod +x ${HELM_BIN_DIR}/helm
        rm -rf "${TD}" && \
        touch "${HELM_BIN_DIR}/version.helm.${HELM_VER}"
fi

# Step2. generate yaml in folder
if [[ ! -d ${RELEASE_FOLDER} ]];then
    mkdir ${RELEASE_FOLDER}
fi

DEPLOYMENT_FILE=${RELEASE_FOLDER}/${YAML_FILENAME}
echo "Generating volcano yaml file into ${DEPLOYMENT_FILE}}"

if [[ -f ${DEPLOYMENT_FILE} ]];then
    rm ${DEPLOYMENT_FILE}
fi
cat ${VK_ROOT}/installer/namespace.yaml > ${DEPLOYMENT_FILE}
${HELM_BIN_DIR}/helm template ${VK_ROOT}/installer/helm/chart/volcano --namespace volcano-system \
      --name volcano --set basic.image_tag_version=${VOLCANO_IMAGE_TAG} \
      --set basic.scheduler_config_file=kube-batch-ci.conf \
      -x templates/admission.yaml \
      -x templates/batch_v1alpha1_job.yaml \
      -x templates/bus_v1alpha1_command.yaml \
      -x templates/controllers.yaml \
      -x templates/scheduler.yaml \
      -x templates/scheduling_v1alpha1_podgroup.yaml \
      -x templates/scheduling_v1alpha1_queue.yaml \
      -x templates/scheduling_v1alpha2_podgroup.yaml \
      -x templates/scheduling_v1alpha2_queue.yaml \
      --notes >> ${DEPLOYMENT_FILE}
