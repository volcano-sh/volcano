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
    GOOS=${OS} go install sigs.k8s.io/kind@v0.31.0
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
  local required_version
  required_version=$(go list -m -f '{{.Version}}' github.com/onsi/ginkgo/v2 2>/dev/null)
  if [[ -z "${required_version}" ]]; then
    echo -e "\033[31mERROR\033[0m: failed to resolve required ginkgo version from go.mod"
    exit 1
  fi

  if command -v ginkgo >/dev/null 2>&1; then
    local found_version
    found_version=$(ginkgo version 2>/dev/null | awk '{print $3}')
    found_version=${found_version#v}
    local normalized_required=${required_version#v}
    if [[ -z "${found_version}" ]]; then
      echo "Unable to determine installed ginkgo version, reinstalling..."
    elif [[ "${found_version}" == "${normalized_required}" ]]; then
      echo "Found ginkgo, version: ${found_version}"
      return
    else
      echo "Ginkgo version mismatch (found ${found_version}, required ${normalized_required}), reinstalling..."
    fi
  else
    echo "Installing ginkgo ..."
  fi

  GOOS=${OS} go install github.com/onsi/ginkgo/v2/ginkgo@${required_version}
  # This file is sourced (e.g. from hack/run-e2e-kind.sh), so updating PATH here
  # ensures subsequent ginkgo invocations in the calling script use the installed binary.
  local bin_path
  bin_path=$(go env GOBIN)
  if [[ -z "${bin_path}" ]]; then
    bin_path="$(go env GOPATH)/bin"
  fi
  export PATH="${bin_path}:${PATH}"
  echo -n "Using ginkgo, version: " && ginkgo version
}

function install-kwok-with-helm {
  helm repo add kwok https://kwok.sigs.k8s.io/charts/
  helm repo update
  helm upgrade --namespace kube-system --install kwok kwok/kwok
  helm upgrade --install kwok kwok/stage-fast
  # delete pod-complete stage to avoid volcano-job-pod change status to complete.
  kubectl delete stage pod-complete
}
