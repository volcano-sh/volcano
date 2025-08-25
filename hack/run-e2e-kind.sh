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
export CLEANUP_CLUSTER=${CLEANUP_CLUSTER:-1}
export E2E_TYPE=${E2E_TYPE:-"ALL"}
export ARTIFACTS_PATH=${ARTIFACTS_PATH:-"${VK_ROOT}/volcano-e2e-logs"}
mkdir -p "$ARTIFACTS_PATH"

NAMESPACE=${NAMESPACE:-volcano-system}
CLUSTER_NAME=${CLUSTER_NAME:-integration}

export CLUSTER_CONTEXT=("--name" "${CLUSTER_NAME}")

export KIND_OPT=${KIND_OPT:="--config ${VK_ROOT}/hack/e2e-kind-config.yaml"}

# kwok node config
export KWOK_NODE_CPU=${KWOK_NODE_CPU:-8}      # 8 cores
export KWOK_NODE_MEMORY=${KWOK_NODE_MEMORY:-8Gi}  # 8GB

# create kwok node
function create-kwok-node() {
  local node_index=$1
  
  kubectl apply -f - <<EOF
apiVersion: v1
kind: Node
metadata:
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
    kwok.x-k8s.io/node: fake
  labels:
    beta.kubernetes.io/arch: amd64
    beta.kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    kubernetes.io/hostname: kwok-node-${node_index}
    kubernetes.io/os: linux
    kubernetes.io/role: agent
    node-role.kubernetes.io/agent: ""
    type: kwok
  name: kwok-node-${node_index}
spec:
  taints:
  - effect: NoSchedule
    key: kwok.x-k8s.io/node
    value: fake
status:
  capacity:
    cpu: "${KWOK_NODE_CPU}"
    memory: "${KWOK_NODE_MEMORY}"
    pods: "110"
  allocatable:
    cpu: "${KWOK_NODE_CPU}"
    memory: "${KWOK_NODE_MEMORY}"
    pods: "110"
EOF
}

# install kwok nodes
function install-kwok-nodes() {
  local node_count=$1
  for i in $(seq 0 $((node_count-1))); do
    create-kwok-node $i
  done
}

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
  cat <<EOF | helm install ${CLUSTER_NAME} installer/helm/chart/volcano \
  --namespace ${NAMESPACE} \
  --kubeconfig ${KUBECONFIG} \
  --values - \
  --wait
basic:
  image_pull_policy: IfNotPresent
  image_tag_version: ${TAG}
  scheduler_config_file: config/volcano-scheduler-ci.conf
  crd_version: ${crd_version}

custom:
  scheduler_log_level: 5
  admission_tolerations:
    - key: "node-role.kubernetes.io/control-plane"
      operator: "Exists"
      effect: "NoSchedule"
    - key: "node-role.kubernetes.io/master"
      operator: "Exists"
      effect: "NoSchedule"
  controller_tolerations:
    - key: "node-role.kubernetes.io/control-plane"
      operator: "Exists"
      effect: "NoSchedule"
    - key: "node-role.kubernetes.io/master"
      operator: "Exists"
      effect: "NoSchedule"
  scheduler_tolerations:
    - key: "node-role.kubernetes.io/control-plane"
      operator: "Exists"
      effect: "NoSchedule"
    - key: "node-role.kubernetes.io/master"
      operator: "Exists"
      effect: "NoSchedule"
  default_ns:
    node-role.kubernetes.io/control-plane: ""
  scheduler_feature_gates: ${FEATURE_GATES}
  ignored_provisioners: ${IGNORED_PROVISIONERS:-""}
EOF
}

function uninstall-volcano {
  helm uninstall "${CLUSTER_NAME}" -n ${NAMESPACE}
}

function generate-log {
    echo "Generating volcano log files"
    kind export logs "${CLUSTER_CONTEXT[@]}" "$ARTIFACTS_PATH"
}

# clean up
function cleanup {
  uninstall-volcano

  echo "Running kind: [kind delete cluster ${CLUSTER_CONTEXT[*]}]"
  kind delete cluster "${CLUSTER_CONTEXT[@]}"
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
install-kwok-with-helm

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
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/cronjob/
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress --focus="DRA E2E Test" ./test/e2e/dra/
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/hypernode/
    ;;
"JOBP")
    echo "Running parallel job e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -v -r --nodes=4 --compilers=4 --randomize-all --randomize-suites --fail-on-pending --cover --trace --race --slow-spec-threshold='30s' --progress ./test/e2e/jobp/
    ;;
"JOBSEQ")
    echo "Running sequence job e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -v -r --slow-spec-threshold='30s' --progress ./test/e2e/jobseq/
    ;;
"SCHEDULINGBASE")
    echo "Running scheduling base e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -v -r --slow-spec-threshold='30s' --progress ./test/e2e/schedulingbase/
    ;;
"SCHEDULINGACTION")
    echo "Running scheduling action e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -v -r --slow-spec-threshold='30s' --progress ./test/e2e/schedulingaction/
    ;;
"VCCTL")
    echo "Running vcctl e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -v -r --slow-spec-threshold='30s' --progress ./test/e2e/vcctl/
    ;;
"STRESS")
    echo "Running stress e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -v -r --slow-spec-threshold='30s' --progress ./test/e2e/stress/
    ;;
"DRA")
    echo "Running dra e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -v -r --slow-spec-threshold='30s' --progress --focus="DRA E2E Test" ./test/e2e/dra/
    ;;
"HYPERNODE")
    echo "Creating 8 kwok nodes for 3-tier topology"
    install-kwok-nodes 8
    echo "Running hypernode e2e suite..."
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -r --slow-spec-threshold='30s' --progress ./test/e2e/hypernode/
    ;;
"CRONJOB")  
    echo "Running cronjob e2e suite..."  
    KUBECONFIG=${KUBECONFIG} GOOS=${OS} ginkgo -v -r --slow-spec-threshold='30s' --progress ./test/e2e/cronjob/  
    ;;
esac

if [[ $? -ne 0 ]]; then
  generate-log
  exit 1
fi
