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

  echo
  check-images

  echo
  echo "Loading docker images into kind cluster"
  kind load docker-image ${IMAGE_PREFIX}-controller-manager:${TAG} ${CLUSTER_CONTEXT}
  kind load docker-image ${IMAGE_PREFIX}-scheduler:${TAG} ${CLUSTER_CONTEXT}
  kind load docker-image ${IMAGE_PREFIX}-webhook-manager:${TAG} ${CLUSTER_CONTEXT}
}

# check if the required images exist
function check-images {
  echo "Checking whether the required images exist"
  docker image inspect "${IMAGE_PREFIX}-controller-manager:${TAG}" > /dev/null
  if [[ $? -ne 0 ]]; then
    echo -e "\033[31mERROR\033[0m: ${IMAGE_PREFIX}-controller-manager:${TAG} does not exist"
    exit 1
  fi
  docker image inspect "${IMAGE_PREFIX}-scheduler:${TAG}" > /dev/null
  if [[ $? -ne 0 ]]; then
    echo -e "\033[31mERROR\033[0m: ${IMAGE_PREFIX}-scheduler:${TAG} does not exist"
    exit 1
  fi
  docker image inspect "${IMAGE_PREFIX}-webhook-manager:${TAG}" > /dev/null
  if [[ $? -ne 0 ]]; then
    echo -e "\033[31mERROR\033[0m: ${IMAGE_PREFIX}-webhook-manager:${TAG} does not exist"
    exit 1
  fi
}

# check if kubectl installed
function check-prerequisites {
  echo "Checking prerequisites"
  which kubectl >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo -e "\033[31mERROR\033[0m: kubectl not installed"
    exit 1
  else
    echo -n "Found kubectl, version: " && kubectl version --short --client
  fi
}

# check if kind installed
function check-kind {
  echo "Checking kind"
  which kind >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo "Installing kind ..."
    GO111MODULE="on" go get sigs.k8s.io/kind@v0.6.1
  else
    echo -n "Found kind, version: " && kind version
  fi
}

# install helm if not installed
function install-helm {
  echo "Checking helm"
  which helm >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo "Installing helm via script"
    HELM_TEMP_DIR=$(mktemp -d)
    curl https://raw.githubusercontent.com/helm/helm/master/scripts/get > ${HELM_TEMP_DIR}/get_helm.sh
    # TODO: There are some issue with helm's latest version, remove '--version' when it get fixed.
    chmod 700 ${HELM_TEMP_DIR}/get_helm.sh && ${HELM_TEMP_DIR}/get_helm.sh --version v3.0.1
  else
    HELM_VERSION_DETAILS=$(helm version)
    echo -n "Found helm, version: $HELM_VERSION_DETAILS"
    HELM_VERSION=$(echo $HELM_VERSION_DETAILS | grep -Eo "v[0-9](.[0-9])+")
    DESIRED_VERSION="v3.0.1"
    if [ "$HELM_VERSION" != "$DESIRED_VERSION" ]; then
      wget https://get.helm.sh/helm-v3.0.1-linux-amd64.tar.gz
      tar -zxvf helm-v3.0.1-linux-amd64.tar.gz
      mv linux-amd64/helm /usr/bin/helm
    fi
  fi
}
