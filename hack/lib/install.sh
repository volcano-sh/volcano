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

# spin up cluster with kind command
function kind-up-cluster {
  check-kind
  echo "Running kind: [kind create cluster ${CLUSTER_CONTEXT} ${KIND_OPT}]"
  kind create cluster ${CLUSTER_CONTEXT} ${KIND_OPT}

  echo "Loading docker images into kind cluster"
  kind load docker-image ${IMAGE_PREFIX}-controller-manager:${TAG}  ${CLUSTER_CONTEXT}
  kind load docker-image ${IMAGE_PREFIX}-scheduler:${TAG}  ${CLUSTER_CONTEXT}
  kind load docker-image ${IMAGE_PREFIX}-webhook-manager:${TAG}  ${CLUSTER_CONTEXT}
}


# check if kubectl installed
function check-prerequisites {
  echo "checking prerequisites"
  which kubectl >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo "kubectl not installed, exiting."
    exit 1
  else
    echo -n "found kubectl, " && kubectl version --short --client
  fi
}

# check if kind installed
function check-kind {
  echo "checking kind"
  which kind >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo "installing kind ."
    GO111MODULE="on" go get sigs.k8s.io/kind@v0.6.1
  else
    echo -n "found kind, version: " && kind version
  fi
}

# install helm if not installed
function install-helm {
  echo "checking helm"
  which helm >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo "Install helm via script"
    HELM_TEMP_DIR=`mktemp -d`
    curl https://raw.githubusercontent.com/helm/helm/master/scripts/get > ${HELM_TEMP_DIR}/get_helm.sh
    #TODO: There are some issue with helm's latest version, remove '--version' when it get fixed.
    chmod 700 ${HELM_TEMP_DIR}/get_helm.sh && ${HELM_TEMP_DIR}/get_helm.sh   --version v3.5.3
  else
    echo -n "found helm, version: " && helm version
  fi
}
