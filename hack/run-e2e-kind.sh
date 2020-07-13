#!/bin/bash

# Copyright 2020 The Volcano Authors.

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

export VK_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
export VC_BIN=${VK_ROOT}/${BIN_DIR}/${BIN_OSARCH}
export LOG_LEVEL=3
export CLEANUP_CLUSTER=${CLEANUP_CLUSTER:-1}

if [[ "${CLUSTER_NAME}xxx" == "xxx" ]];then
    CLUSTER_NAME="integration"
fi

export CLUSTER_CONTEXT="--name ${CLUSTER_NAME}"

export KIND_OPT=${KIND_OPT:=" --config ${VK_ROOT}/hack/e2e-kind-config.yaml"}

# spin up cluster with kind command
function kind-up-cluster {
  check-prerequisites
  check-kind
  echo "Running kind: [kind create cluster ${CLUSTER_CONTEXT} ${KIND_OPT}]"
  kind create cluster ${CLUSTER_CONTEXT} ${KIND_OPT}
}

function install-volcano {
  install-helm
  echo "Install volcano chart"
  helm install ${CLUSTER_NAME} installer/helm/chart/volcano --namespace kube-system  --kubeconfig ${KUBECONFIG} --set basic.image_tag_version=${TAG} --set basic.scheduler_config_file=config/volcano-scheduler-ci.conf --wait
}

function uninstall-volcano {
  helm uninstall ${CLUSTER_NAME} -n kube-system
}

function generate-log {
    echo "Generating volcano log files"
    kubectl logs deployment/${CLUSTER_NAME}-admission -n kube-system > volcano-admission.log
    kubectl logs deployment/${CLUSTER_NAME}-controllers -n kube-system > volcano-controller.log
    kubectl logs deployment/${CLUSTER_NAME}-scheduler -n kube-system > volcano-scheduler.log

}

# clean up
function cleanup {
  uninstall-volcano

  echo "Running kind: [kind delete cluster ${CLUSTER_CONTEXT}]"
  kind delete cluster ${CLUSTER_CONTEXT}
}

echo $* | grep -E -q "\-\-help|\-h"
if [[ $? -eq 0 ]]; then
  echo "Customize the kind-cluster name:

    export CLUSTER_NAME=<custom cluster name>  # default: integration

Customize kind options other than --name:

    export KIND_OPT=<kind options>
"
  exit 0
fi

if [[ $CLEANUP_CLUSTER -eq 1 ]]; then
    trap cleanup EXIT
fi

source "${VK_ROOT}/hack/lib/install.sh"

kind-up-cluster
export KUBECONFIG="${HOME}/.kube/config"
install-volcano

# Run e2e test
cd ${VK_ROOT}
KUBECONFIG=${KUBECONFIG} go test ./test/e2e -v -timeout 60m

if [[ $? != 0 ]]; then
  generate-log
  exit 1
fi
