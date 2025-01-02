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

export VK_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
export VC_BIN=${VK_ROOT}/${BIN_DIR}/${BIN_OSARCH}
export LOG_LEVEL=3
export SHOW_VOLCANO_LOGS=${SHOW_VOLCANO_LOGS:-1}
export CLEANUP_CLUSTER=${CLEANUP_CLUSTER:-1}
export E2E_TYPE=${E2E_TYPE:-"ALL"}

NAMESPACE=${NAMESPACE:-volcano-system}
CLUSTER_NAME=${CLUSTER_NAME:-integration}

export CLUSTER_CONTEXT="--name ${CLUSTER_NAME}"

export KIND_OPT=${KIND_OPT:="--config ${VK_ROOT}/hack/e2e-kind-config.yaml"}


function install-volcano {
  install-helm

  # judge crd version
  major=$(kubectl version --output yaml | awk '/serverVersion/,0' |grep -E 'major:' | awk '{print $2}' | tr "\"" " ")
  minor=$(kubectl version --output yaml | awk '/serverVersion/,0' |grep -E 'minor:' | awk '{print $2}' | tr "\"" " ")
  crd_version="v1"
  # if k8s version less than v1.18, crd version use v1beta
  if [ "$major" -le "1" ]; then
    if [ "$minor" -lt "18" ]; then
      crd_version="v1beta1"
    fi
  fi

  echo "Ensure create namespace"
  kubectl apply -f installer/namespace.yaml

  echo "Install volcano chart with crd version $crd_version"
  helm install ${CLUSTER_NAME} installer/helm/chart/volcano --namespace ${NAMESPACE} --kubeconfig ${KUBECONFIG} \
    --set basic.image_pull_policy=IfNotPresent \
    --set basic.image_tag_version=${TAG} \
    --set basic.scheduler_config_file=config/volcano-scheduler-ci.conf \
    --set basic.crd_version=${crd_version} \
    --wait
}

function uninstall-volcano {
  helm uninstall ${CLUSTER_NAME} -n ${NAMESPACE}
}

function generate-log {
    echo "Generating volcano log files"
    kubectl logs deployment/${CLUSTER_NAME}-admission -n ${NAMESPACE} > volcano-admission.log
    kubectl logs deployment/${CLUSTER_NAME}-controllers -n ${NAMESPACE} > volcano-controller.log
    kubectl logs deployment/${CLUSTER_NAME}-scheduler -n ${NAMESPACE} > volcano-scheduler.log
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

install-ginkgo-if-not-exist

case ${E2E_TYPE} in
"ALL")
    echo "Running e2e..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --nodes=4 --compilers=4 --randomize-all --randomize-suites --fail-on-pending --cover --trace --race --slow-spec-threshold='30s' --progress ./test/e2e/jobp/
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/jobseq/
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/schedulingbase/
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/schedulingaction/
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/vcctl/
    ;;
"JOBP")
    echo "Running parallel job e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --nodes=4 --compilers=4 --randomize-all --randomize-suites --fail-on-pending --cover --trace --race --slow-spec-threshold='30s' --progress ./test/e2e/jobp/
    ;;
"JOBSEQ")
    echo "Running sequence job e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/jobseq/
    ;;
"SCHEDULINGBASE")
    echo "Running scheduling base e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/schedulingbase/
    ;;
"SCHEDULINGACTION")
    echo "Running scheduling action e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/schedulingaction/
    ;;
"VCCTL")
    echo "Running vcctl e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/vcctl/
    ;;
"STRESS")
    echo "Running stress e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/stress/
    ;;
esac

if [[ $? -ne 0 ]]; then
  generate-log
  exit 1
fi
