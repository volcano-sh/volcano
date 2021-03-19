#!/bin/bash

#
# Copyright 2021 The Volcano Authors.
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

export HELM_VER=${HELM_VER:-v2.13.0}
export VOLCANO_IMAGE_TAG=${TAG:-"latest"}
export YAML_FILENAME=volcano-${VOLCANO_IMAGE_TAG}.yaml
export MONITOR_YAML_FILENAME=volcano-monitoring-${VOLCANO_IMAGE_TAG}.yaml

CRD_VERSION=${CRD_VERSION:-"v1beta1"}

case $CRD_VERSION in
  "v1")
    CRD_DIR="bases"
    ;;
  "v1beta1")
    CRD_DIR="v1beta1"
    ;;
  *)
    echo Invaild CRD_VERSION !!!
    echo CRD_VERSION only support \"v1\" and \"v1beta1\"
    exit 1
    ;;
  esac

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

# Step2. update helm templates from config dir
HELM_TEMPLATES_DIR=${VK_ROOT}/installer/helm/chart/volcano/templates
echo Updating templates in $HELM_TEMPLATES_DIR
# use tail because we should skip top two line
tail -n +3 ${VK_ROOT}/config/crd/${CRD_DIR}/batch.volcano.sh_jobs.yaml > ${HELM_TEMPLATES_DIR}/batch_v1alpha1_job.yaml
tail -n +3 ${VK_ROOT}/config/crd/${CRD_DIR}/bus.volcano.sh_commands.yaml > ${HELM_TEMPLATES_DIR}/bus_v1alpha1_command.yaml
tail -n +3 ${VK_ROOT}/config/crd/${CRD_DIR}/scheduling.volcano.sh_podgroups.yaml > ${HELM_TEMPLATES_DIR}/scheduling_v1beta1_podgroup.yaml
tail -n +3 ${VK_ROOT}/config/crd/${CRD_DIR}/scheduling.volcano.sh_queues.yaml > ${HELM_TEMPLATES_DIR}/scheduling_v1beta1_queue.yaml

# Step3. generate yaml in folder
if [[ ! -d ${RELEASE_FOLDER} ]];then
    mkdir ${RELEASE_FOLDER}
fi

DEPLOYMENT_FILE=${RELEASE_FOLDER}/${YAML_FILENAME}
MONITOR_DEPLOYMENT_YAML_FILENAME=${RELEASE_FOLDER}/${MONITOR_YAML_FILENAME}
echo "Generating volcano yaml file into ${DEPLOYMENT_FILE}"

if [[ -f ${DEPLOYMENT_FILE} ]];then
    rm ${DEPLOYMENT_FILE}
fi
cat ${VK_ROOT}/installer/namespace.yaml > ${DEPLOYMENT_FILE}
${HELM_BIN_DIR}/helm template ${VK_ROOT}/installer/helm/chart/volcano --namespace volcano-system \
      --name volcano --set basic.image_tag_version=${VOLCANO_IMAGE_TAG} \
      -x templates/admission.yaml \
      -x templates/batch_v1alpha1_job.yaml \
      -x templates/bus_v1alpha1_command.yaml \
      -x templates/controllers.yaml \
      -x templates/scheduler.yaml \
      -x templates/scheduling_v1beta1_podgroup.yaml \
      -x templates/scheduling_v1beta1_queue.yaml \
      --notes >> ${DEPLOYMENT_FILE}

${HELM_BIN_DIR}/helm template ${VK_ROOT}/installer/helm/chart/volcano --namespace volcano-monitoring \
      --name volcano --set basic.image_tag_version=${VOLCANO_IMAGE_TAG} \
      -x templates/prometheus.yaml \
      -x templates/kubestatemetrics.yaml \
      -x templates/grafana.yaml \
      --notes >> ${MONITOR_DEPLOYMENT_YAML_FILENAME}