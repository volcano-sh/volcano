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
function kind-create-cluster {
  local kind_cmd=${KIND_BIN:-kind}

  check-kind

  local cluster_name=${CLUSTER_CONTEXT[1]}

  if ${kind_cmd} get clusters 2>/dev/null | grep -qw "${cluster_name}"; then
    echo "Kind cluster ${cluster_name} already exists"
  else
    echo "Running kind: [${kind_cmd} create cluster ${CLUSTER_CONTEXT[*]} ${KIND_OPT}]"
    ${kind_cmd} create cluster "${CLUSTER_CONTEXT[@]}" ${KIND_OPT}
  fi

  echo "Exporting kubeconfig for kind cluster ${cluster_name}"
  ${kind_cmd} export kubeconfig --name "${cluster_name}" >/dev/null
}

function kind-up-cluster {
  kind-create-cluster

  echo
  check-images

  echo
  echo "Loading docker images into kind cluster"
  # only need to load images into control-plane node because volcano components are deployed on control-plane node.
  ${KIND_BIN:-kind} load docker-image ${IMAGE_PREFIX}/vc-controller-manager:${TAG} "${CLUSTER_CONTEXT[@]}" --nodes ${CLUSTER_CONTEXT[1]}-control-plane
  ${KIND_BIN:-kind} load docker-image ${IMAGE_PREFIX}/vc-scheduler:${TAG}          "${CLUSTER_CONTEXT[@]}" --nodes ${CLUSTER_CONTEXT[1]}-control-plane
  ${KIND_BIN:-kind} load docker-image ${IMAGE_PREFIX}/vc-webhook-manager:${TAG}    "${CLUSTER_CONTEXT[@]}" --nodes ${CLUSTER_CONTEXT[1]}-control-plane
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
  local kind_cmd=${KIND_BIN:-kind}
  local required_version=${KIND_VERSION}

  # If KIND_VERSION is not set, try to resolve it from Makefile in VK_ROOT
  if [[ -z "${required_version}" ]] && [[ -n "${VK_ROOT}" ]] && [[ -f "${VK_ROOT}/Makefile" ]]; then
    required_version=$(make -s -C "${VK_ROOT}" print-kind-version 2>/dev/null)
  fi

  if [[ -z "${required_version}" ]]; then
    echo "ERROR: KIND_VERSION is not set and could not be resolved from Makefile"
    return 1
  fi

  echo "Checking kind"
  if command -v "${kind_cmd}" >/dev/null 2>&1; then
    local found_version
    found_version=$("${kind_cmd}" version | grep -Eo 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)
    if [[ "${found_version}" == "${required_version}" ]]; then
      echo "Found kind at ${kind_cmd}, version: ${found_version}"
      return
    fi

    echo "Kind version mismatch at ${kind_cmd} (found ${found_version}, required ${required_version})"
  else
    echo "Kind not found at ${kind_cmd}"
  fi

  if [[ -n "${KIND_BIN}" ]]; then
    local os arch
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    arch=$(uname -m)
    case "${arch}" in
      x86_64) arch=amd64 ;;
      aarch64|arm64) arch=arm64 ;;
    esac

    echo "Installing kind ${required_version} to ${KIND_BIN} ..."
    mkdir -p "$(dirname "${KIND_BIN}")"
    curl -fsSL "https://github.com/kubernetes-sigs/kind/releases/download/${required_version}/kind-${os}-${arch}" -o "${KIND_BIN}"
    chmod +x "${KIND_BIN}"
    echo "Installed kind at ${KIND_BIN}"
  else
    echo "Installing kind ${required_version} via go install ..."
    GOOS=${OS} go install sigs.k8s.io/kind@${required_version}
    echo -n "Installed kind, version: " && kind version
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
