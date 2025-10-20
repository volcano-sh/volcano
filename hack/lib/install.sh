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

  echo "Running kind: [kind create cluster ${CLUSTER_CONTEXT[*]} ${KIND_OPT}]"
  kind create cluster "${CLUSTER_CONTEXT[@]}" ${KIND_OPT}

  echo
  check-images

  echo
  echo "Loading docker images into kind cluster"
  # only need to load images into control-plane node because volcano components are deployed on control-plane node.
  kind load docker-image ${IMAGE_PREFIX}/vc-controller-manager:${TAG} "${CLUSTER_CONTEXT[@]}" --nodes ${CLUSTER_CONTEXT[1]}-control-plane
  kind load docker-image ${IMAGE_PREFIX}/vc-scheduler:${TAG}          "${CLUSTER_CONTEXT[@]}" --nodes ${CLUSTER_CONTEXT[1]}-control-plane
  kind load docker-image ${IMAGE_PREFIX}/vc-webhook-manager:${TAG}    "${CLUSTER_CONTEXT[@]}" --nodes ${CLUSTER_CONTEXT[1]}-control-plane
}

# check if the required images exist
function check-images {
  echo "Checking whether the required images exist"
  docker image inspect "${IMAGE_PREFIX}/vc-controller-manager:${TAG}" > /dev/null
  if [[ $? -ne 0 ]]; then
    echo -e "\033[31mERROR\033[0m: ${IMAGE_PREFIX}/vc-controller-manager:${TAG} does not exist"
    exit 1
  fi
  docker image inspect "${IMAGE_PREFIX}/vc-scheduler:${TAG}" > /dev/null
  if [[ $? -ne 0 ]]; then
    echo -e "\033[31mERROR\033[0m: ${IMAGE_PREFIX}/vc-scheduler:${TAG} does not exist"
    exit 1
  fi
  docker image inspect "${IMAGE_PREFIX}/vc-webhook-manager:${TAG}" > /dev/null
  if [[ $? -ne 0 ]]; then
    echo -e "\033[31mERROR\033[0m: ${IMAGE_PREFIX}/vc-webhook-manager:${TAG} does not exist"
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
    echo -n "Found kubectl, version: " && kubectl version --client
  fi
}

# check if kind installed
function check-kind {
  echo "Checking kind"
  which kind >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo "Installing kind ..."
    GOOS=${OS} go install sigs.k8s.io/kind@v0.30.0
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
    curl -fsSL -o ${HELM_TEMP_DIR}/get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
    chmod 700 ${HELM_TEMP_DIR}/get_helm.sh && ${HELM_TEMP_DIR}/get_helm.sh
  else
    echo -n "Found helm, version: " && helm version
  fi
}

function install-ginkgo-if-not-exist {
  echo "Checking ginkgo"
  which ginkgo >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo "Installing ginkgo ..."
    GOOS=${OS} go install github.com/onsi/ginkgo/v2/ginkgo
  else
    echo -n "Found ginkgo, version: " && ginkgo version
  fi
}

function install-kwok-with-helm {
  helm repo add kwok https://kwok.sigs.k8s.io/charts/
  helm repo update
  helm upgrade --namespace kube-system --install kwok kwok/kwok
  helm upgrade --install kwok kwok/stage-fast
  # delete pod-complete stage to avoid volcano-job-pod change status to complete.
  kubectl delete stage pod-complete
}

function install-fake-gpu-operator {
  if [[ ${PULL_IMAGES} == true ]]; then
    # download images
    docker pull ghcr.io/run-ai/fake-gpu-operator/device-plugin:0.0.63
    docker pull ghcr.io/run-ai/fake-gpu-operator/status-updater:0.0.63
    docker pull ghcr.io/run-ai/fake-gpu-operator/status-exporter:0.0.63
    docker pull ghcr.io/run-ai/fake-gpu-operator/topology-server:0.0.63
    docker pull ghcr.io/run-ai/fake-gpu-operator/mig-faker:0.0.63
    docker pull ghcr.io/run-ai/fake-gpu-operator/kwok-gpu-device-plugin:0.0.63
    docker pull ubuntu:24.04
    docker pull nginx:latest

    # load images into kind cluster
    kind load docker-image ghcr.io/run-ai/fake-gpu-operator/device-plugin:0.0.63 "${CLUSTER_CONTEXT[@]}"
    kind load docker-image ghcr.io/run-ai/fake-gpu-operator/status-updater:0.0.63 "${CLUSTER_CONTEXT[@]}"
    kind load docker-image ghcr.io/run-ai/fake-gpu-operator/status-exporter:0.0.63 "${CLUSTER_CONTEXT[@]}"
    kind load docker-image ghcr.io/run-ai/fake-gpu-operator/topology-server:0.0.63 "${CLUSTER_CONTEXT[@]}"
    kind load docker-image ghcr.io/run-ai/fake-gpu-operator/mig-faker:0.0.63 "${CLUSTER_CONTEXT[@]}"
    kind load docker-image ghcr.io/run-ai/fake-gpu-operator/kwok-gpu-device-plugin:0.0.63 "${CLUSTER_CONTEXT[@]}"
    kind load docker-image ubuntu:24.04 "${CLUSTER_CONTEXT[@]}"
    kind load docker-image nginx:latest "${CLUSTER_CONTEXT[@]}"
  fi

  # label nodes
  kubectl label node "${CLUSTER_CONTEXT[1]}-worker" run.ai/simulated-gpu-node-pool=card1
  kubectl label node "${CLUSTER_CONTEXT[1]}-worker2" run.ai/simulated-gpu-node-pool=card2
  kubectl label node "${CLUSTER_CONTEXT[1]}-worker3" run.ai/simulated-gpu-node-pool=card3

  # install fake-gpu-operator
  helm upgrade --namespace gpu-operator --install fake-gpu-operator oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator --version 0.0.63 -f ${FAKE_GPU_OPERATOR_VALUES} --create-namespace

  # check resources
  check-fake-gpu-operator
}

# check if fake gpu operator is ready
function check-fake-gpu-operator {
  echo "Checking fake gpu operator"
  # retry get gpu resources on nodes
  while true; do
    if [[ $(kubectl get nodes -l nvidia.com/gpu.present=true --no-headers | wc -l) -eq 3 ]]; then
      break
    fi
    sleep 1
  done
}


function uninstall-fake-gpu-operator {
  helm uninstall fake-gpu-operator -n gpu-operator
}