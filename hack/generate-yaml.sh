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

export HELM_VER=${HELM_VER:-v3.6.3}
export VOLCANO_IMAGE_TAG=${TAG:-"latest"}
export YAML_FILENAME=volcano-${VOLCANO_IMAGE_TAG}.yaml
export MONITOR_YAML_FILENAME=volcano-monitoring-${VOLCANO_IMAGE_TAG}.yaml
export AGENT_YAML_FILENAME=volcano-agent-${VOLCANO_IMAGE_TAG}.yaml

export CRD_VERSION=${CRD_VERSION:-v1}

case $CRD_VERSION in
  bases)
    ;;
  v1)
    CRD_VERSION="bases"
    ;;
  v1beta1)
    ;;
  *)
    echo Invalid CRD_VERSION $CRD_VERSION !!!
    echo CRD_VERSION only support \"bases\", \"v1\" and \"v1beta1\"
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

# Step2. update helm templates from config dir
HELM_TEMPLATES_DIR=${VK_ROOT}/installer/helm/chart/volcano/templates
HELM_VOLCANO_CRD_DIR=${VK_ROOT}/installer/helm/chart/volcano/crd
HELM_JOBFLOW_CRD_DIR=${VK_ROOT}/installer/helm/chart/volcano/charts/jobflow/crd
VOLCANO_CRD_DIR=${VK_ROOT}/config/crd/volcano
JOBFLOW_CRD_DIR=${VK_ROOT}/config/crd/jobflow
echo Updating templates in $HELM_TEMPLATES_DIR
# use tail because we should skip top two line
# sync volcano bases
tail -n +2 ${VOLCANO_CRD_DIR}/bases/batch.volcano.sh_jobs.yaml > ${HELM_VOLCANO_CRD_DIR}/bases/batch.volcano.sh_jobs.yaml
tail -n +2 ${VOLCANO_CRD_DIR}/bases/bus.volcano.sh_commands.yaml > ${HELM_VOLCANO_CRD_DIR}/bases/bus.volcano.sh_commands.yaml
tail -n +2 ${VOLCANO_CRD_DIR}/bases/scheduling.volcano.sh_podgroups.yaml > ${HELM_VOLCANO_CRD_DIR}/bases/scheduling.volcano.sh_podgroups.yaml
tail -n +2 ${VOLCANO_CRD_DIR}/bases/scheduling.volcano.sh_queues.yaml > ${HELM_VOLCANO_CRD_DIR}/bases/scheduling.volcano.sh_queues.yaml
tail -n +2 ${VOLCANO_CRD_DIR}/bases/nodeinfo.volcano.sh_numatopologies.yaml > ${HELM_VOLCANO_CRD_DIR}/bases/nodeinfo.volcano.sh_numatopologies.yaml
tail -n +2 ${VOLCANO_CRD_DIR}/bases/topology.volcano.sh_hypernodes.yaml > ${HELM_VOLCANO_CRD_DIR}/bases/topology.volcano.sh_hypernodes.yaml

# sync jobflow bases
tail -n +2 ${JOBFLOW_CRD_DIR}/bases/flow.volcano.sh_jobflows.yaml > ${HELM_JOBFLOW_CRD_DIR}/bases/flow.volcano.sh_jobflows.yaml
tail -n +2 ${JOBFLOW_CRD_DIR}/bases/flow.volcano.sh_jobtemplates.yaml > ${HELM_JOBFLOW_CRD_DIR}/bases/flow.volcano.sh_jobtemplates.yaml

# Step3. generate yaml in folder
if [[ ! -d ${RELEASE_FOLDER} ]];then
    mkdir ${RELEASE_FOLDER}
fi

DEPLOYMENT_FILE=${RELEASE_FOLDER}/${YAML_FILENAME}
MONITOR_DEPLOYMENT_YAML_FILENAME=${RELEASE_FOLDER}/${MONITOR_YAML_FILENAME}
AGENT_DEPLOYMENT_YAML_FILENAME=${RELEASE_FOLDER}/${AGENT_YAML_FILENAME}

echo "Generating volcano yaml file into ${DEPLOYMENT_FILE}"

if [[ -f ${DEPLOYMENT_FILE} ]];then
    rm ${DEPLOYMENT_FILE}
fi

if [[ -f ${MONITOR_DEPLOYMENT_YAML_FILENAME} ]];then
    rm ${MONITOR_DEPLOYMENT_YAML_FILENAME}
fi

if [[ -f ${AGENT_DEPLOYMENT_YAML_FILENAME} ]];then
    rm "${AGENT_DEPLOYMENT_YAML_FILENAME}"
fi

# Namespace
cat ${VK_ROOT}/installer/namespace.yaml > ${DEPLOYMENT_FILE}

# Volcano
${HELM_BIN_DIR}/helm template ${VK_ROOT}/installer/helm/chart/volcano --namespace volcano-system \
      --name-template volcano --set basic.image_tag_version=${VOLCANO_IMAGE_TAG} --set basic.crd_version=${CRD_VERSION}\
      -s templates/admission.yaml \
      -s templates/admission-init.yaml \
      -s templates/batch_v1alpha1_job.yaml \
      -s templates/bus_v1alpha1_command.yaml \
      -s templates/controllers.yaml \
      -s templates/scheduler.yaml \
      -s templates/scheduling_v1beta1_podgroup.yaml \
      -s templates/scheduling_v1beta1_queue.yaml \
      -s templates/nodeinfo_v1alpha1_numatopologies.yaml \
      -s templates/topology_v1alpha1_hypernodes.yaml \
      -s templates/webhooks.yaml \
      >> ${DEPLOYMENT_FILE}

# JobFlow
${HELM_BIN_DIR}/helm template ${VK_ROOT}/installer/helm/chart/volcano/charts/jobflow --namespace volcano-system \
      --name-template volcano --set basic.image_tag_version=${VOLCANO_IMAGE_TAG} --set basic.crd_version=${CRD_VERSION}\
      -s templates/flow_v1alpha1_jobflows.yaml \
      -s templates/flow_v1alpha1_jobtemplates.yaml \
      >> ${DEPLOYMENT_FILE}

# Monitoring
${HELM_BIN_DIR}/helm template ${VK_ROOT}/installer/helm/chart/volcano --namespace volcano-monitoring \
      --name-template volcano --set basic.image_tag_version=${VOLCANO_IMAGE_TAG} --set custom.metrics_enable=true \
      -s templates/prometheus.yaml \
      -s templates/kubestatemetrics.yaml \
      -s templates/grafana.yaml \
      >> ${MONITOR_DEPLOYMENT_YAML_FILENAME}

# Agent
${HELM_BIN_DIR}/helm template ${VK_ROOT}/installer/helm/chart/volcano --namespace volcano-system \
      --name-template volcano --set basic.image_tag_version=${VOLCANO_IMAGE_TAG} --set custom.colocation_enable=true \
      -s templates/agent.yaml \
      >> ${AGENT_DEPLOYMENT_YAML_FILENAME}
