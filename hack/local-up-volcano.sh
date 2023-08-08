#!/bin/bash

# Copyright 2019 The Volcano Authors.
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

VK_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
CLUSTER_NAME=${CLUSTER_NAME:-volcano}
CLUSTER_CONTEXT="--name ${CLUSTER_NAME}"
KIND_OPT=${KIND_OPT:-}
INSTALL_MODE=${INSTALL_MODE:-"kind"}
VOLCANO_NAMESPACE=${VOLCANO_NAMESPACE:-"volcano-system"}
IMAGE_PREFIX=volcanosh
TAG=${TAG:-`git rev-parse --verify HEAD`}
RELEASE_DIR=_output/release
RELEASE_FOLDER=${VK_ROOT}/${RELEASE_DIR}
YAML_FILENAME=volcano-${TAG}.yaml


# prepare deploy yaml and docker images
function prepare {
  echo "Preparing..."
  install-helm

  echo "Building docker images"
  make images
}


function install-volcano {
  # TODO: add a graceful way waiting for all crd ready
  kubectl create namespace volcano-system 
  helm install volcano ${VK_ROOT}/installer/helm/chart/volcano --namespace volcano-system \
  --set basic.image_tag_version=${TAG} \
  --set basic.image_pull_policy=IfNotPresent 
}

function uninstall-volcano {
  kubectl delete -f ${VK_ROOT}/installer/helm/chart/volcano/templates/scheduling_v1beta1_queue.yaml
  kubectl delete -f ${RELEASE_FOLDER}/${YAML_FILENAME}
}

# clean up
function cleanup {
  uninstall-volcano

  if [ "${INSTALL_MODE}" == "kind" ]; then
    echo "Running kind: [kind delete cluster ${CLUSTER_CONTEXT}]"
    kind delete cluster ${CLUSTER_CONTEXT}
  fi
}

echo $* | grep -E -q "\-\-help|\-h"
if [[ $? -eq 0 ]]; then
  echo "Customize the kind-cluster name:

    export CLUSTER_NAME=<custom cluster name>  # default: volcano

Customize kind options other than --name:

    export KIND_OPT=<kind options>

Using existing kubernetes cluster rather than starting a kind custer:

    export INSTALL_MODE=existing

Cleanup all installation:

    ./hack/local-up-volcano.sh -q
"
  exit 0
fi


echo $* | grep -E -q "\-\-quit|\-q"
if [[ $? -eq 0 ]]; then
  cleanup
  exit 0
fi

source "${VK_ROOT}/hack/lib/install.sh"

check-prerequisites

prepare

if [ "${INSTALL_MODE}" == "kind" ]; then
  kind-up-cluster
  export KUBECONFIG=${HOME}/.kube/config
fi

install-volcano
