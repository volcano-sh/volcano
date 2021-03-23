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
export SHOW_VOLCANO_LOGS=${SHOW_VOLCANO_LOGS:-1}
export CLEANUP_CLUSTER=${CLEANUP_CLUSTER:-1}
export MPI_EXAMPLE_IMAGE=${MPI_EXAMPLE_IMAGE:-"volcanosh/example-mpi:0.0.1"}
export TF_EXAMPLE_IMAGE=${TF_EXAMPLE_IMAGE:-"volcanosh/dist-mnist-tf-example:0.0.1"}
export E2E_TYPE=${E2E_TYPE:-"ALL"}

if [[ "${CLUSTER_NAME}xxx" == "xxx" ]];then
    CLUSTER_NAME="integration"
fi

export CLUSTER_CONTEXT="--name ${CLUSTER_NAME}"

export KIND_OPT=${KIND_OPT:=" --config ${VK_ROOT}/hack/e2e-kind-config.yaml"}

function install-volcano {
  install-helm

  echo "Pulling required docker images"
  docker pull ${MPI_EXAMPLE_IMAGE}
  docker pull ${TF_EXAMPLE_IMAGE}

  echo "Ensure create namespace"
  kubectl apply -f installer/namespace.yaml

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

  if [[ ${SHOW_VOLCANO_LOGS} -eq 1 ]]; then
    #TODO: Add volcano logs support in future.
    echo "Volcano logs are currently not supported."
  fi
}

echo $* | grep -E -q "\-\-help|\-h"
if [[ $? -eq 0 ]]; then
  echo "Customize the kind-cluster name:

    export CLUSTER_NAME=<custom cluster name>  # default: integration

Customize kind options other than --name:

    export KIND_OPT=<kind options>

Disable displaying volcano component logs:

    export SHOW_VOLCANO_LOGS=0
"
  exit 0
fi

if [[ $CLEANUP_CLUSTER -eq 1 ]]; then
    trap cleanup EXIT
fi

source "${VK_ROOT}/hack/lib/install.sh"

check-prerequisites
kind-up-cluster

if [[ -z ${KUBECONFIG+x} ]]; then
    export KUBECONFIG="${HOME}/.kube/config"
fi

install-volcano

# Run e2e test
cd ${VK_ROOT}

GO111MODULE=off go get github.com/onsi/ginkgo/ginkgo

case ${E2E_TYPE} in
"ALL")
    echo "Running e2e..."
    KUBECONFIG=${KUBECONFIG} ginkgo -r --nodes=4 --compilers=4 --randomizeAllSpecs --randomizeSuites --failOnPending --cover --trace --race --slowSpecThreshold=30 --progress ./test/e2e/jobp/
    KUBECONFIG=${KUBECONFIG} ginkgo -r --slowSpecThreshold=30 --progress ./test/e2e/jobseq/
    KUBECONFIG=${KUBECONFIG} ginkgo -r --slowSpecThreshold=30 --progress ./test/e2e/schedulingbase/
    KUBECONFIG=${KUBECONFIG} ginkgo -r --slowSpecThreshold=30 --progress ./test/e2e/schedulingaction/
    KUBECONFIG=${KUBECONFIG} ginkgo -r --slowSpecThreshold=30 --progress ./test/e2e/vcctl/
    ;;
"JOBP")
    echo "Running parallel job e2e suite..."
    KUBECONFIG=${KUBECONFIG} ginkgo -r --nodes=4 --compilers=4 --randomizeAllSpecs --randomizeSuites --failOnPending --cover --trace --race --slowSpecThreshold=30 --progress ./test/e2e/jobp/
    ;;
"JOBSEQ")
    echo "Running sequence job e2e suite..."
    KUBECONFIG=${KUBECONFIG} ginkgo -r --slowSpecThreshold=30 --progress ./test/e2e/jobseq/
    ;;
"SCHEDULINGBASE")
    echo "Running scheduling base e2e suite..."
    KUBECONFIG=${KUBECONFIG} ginkgo -r --slowSpecThreshold=30 --progress ./test/e2e/schedulingbase/
    ;;
"SCHEDULINGACTION")
    echo "Running scheduling action e2e suite..."
    KUBECONFIG=${KUBECONFIG} ginkgo -r --slowSpecThreshold=30 --progress ./test/e2e/schedulingaction/
    ;;
"VCCTL")
    echo "Running vcctl e2e suite..."
    KUBECONFIG=${KUBECONFIG} ginkgo -r --slowSpecThreshold=30 --progress ./test/e2e/vcctl/
    ;;
esac

if [[ $? != 0 ]]; then
  generate-log
  exit 1
fi
