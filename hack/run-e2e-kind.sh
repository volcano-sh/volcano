#!/bin/bash

export VK_ROOT=$(dirname "${BASH_SOURCE}")/..
export VK_BIN=${VK_ROOT}/_output/bin
export LOG_LEVEL=3
export SHOW_VOLCANO_LOGS=${SHOW_VOLCANO_LOGS:-1}

if [ "${CLUSTER_NAME}xxx" != "xxx" ];then
  export CLUSTER_CONTEXT="--name ${CLUSTER_NAME}"
fi

export KIND_OPT=${KIND_OPT:="--image kindest/node:v1.13.2-huawei --config ${VK_ROOT}/hack/e2e-kind-config.yaml"}

export KIND_IMAGE=$(echo ${KIND_OPT} |grep -E -o "image \w+\/[^ ]*" | sed "s/image //")

# check if kind installed
function check-prerequisites {
  echo "checking prerequisites"
  which kind >/dev/null 2>&1
  if [ $? -ne 0 ]; then
    echo "kind not installed, exiting."
    exit 1
  else
    echo -n "found kind, version: " && kind version
  fi

  which kubectl >/dev/null 2>&1
  if [ $? -ne 0 ]; then
    echo "kubectl not installed, exiting."
    exit 1
  else
    echo -n "found kubectl, " && kubectl version --short --client
  fi
}

# check if the images that kind use exists.
function check-kind-image {
  docker images | awk '{print $1":"$2}' | grep -q "${KIND_IMAGE}"
  if [ $? -ne 0 ]; then
    echo "image: ${KIND_IMAGE} not found."
    exit 1
  fi
}

# spin up cluster with kind command
function kind-up-cluster {
  check-prerequisites
  check-kind-image
  echo "Running kind: [kind create cluster ${CLUSTER_CONTEXT} ${KIND_OPT}]"
  kind create cluster ${CLUSTER_CONTEXT} ${KIND_OPT}
}

function install-volcano {
  kubectl --kubeconfig ${KUBECONFIG} create -f ${VK_ROOT}/config/crds/scheduling_v1alpha1_podgroup.yaml
  kubectl --kubeconfig ${KUBECONFIG} create -f ${VK_ROOT}/config/crds/scheduling_v1alpha1_queue.yaml
  kubectl --kubeconfig ${KUBECONFIG} create -f ${VK_ROOT}/config/crds/batch_v1alpha1_job.yaml
  kubectl --kubeconfig ${KUBECONFIG} create -f ${VK_ROOT}/config/crds/bus_v1alpha1_command.yaml

  # TODO: make vk-controllers and vk-scheduler run in container / in k8s
  # start controller
  nohup ${VK_BIN}/vk-controllers --kubeconfig ${KUBECONFIG} --logtostderr --v ${LOG_LEVEL} > controller.log 2>&1 &
  echo $! > vk-controllers.pid

  # start scheduler
  nohup ${VK_BIN}/vk-scheduler --kubeconfig ${KUBECONFIG} --scheduler-conf=example/kube-batch-conf.yaml --logtostderr --v ${LOG_LEVEL} > scheduler.log 2>&1 &
  echo $! > vk-scheduler.pid
}

function uninstall-volcano {
  kubectl --kubeconfig ${KUBECONFIG} delete -f ${VK_ROOT}/config/crds/scheduling_v1alpha1_podgroup.yaml
  kubectl --kubeconfig ${KUBECONFIG} delete -f ${VK_ROOT}/config/crds/scheduling_v1alpha1_queue.yaml
  kubectl --kubeconfig ${KUBECONFIG} delete -f ${VK_ROOT}/config/crds/batch_v1alpha1_job.yaml
  kubectl --kubeconfig ${KUBECONFIG} delete -f ${VK_ROOT}/config/crds/bus_v1alpha1_command.yaml

  kill -9 $(cat vk-controllers.pid)
  kill -9 $(cat vk-scheduler.pid)
  rm vk-controllers.pid vk-scheduler.pid
}

# clean up
function cleanup {
  uninstall-volcano

  echo "Running kind: [kind delete cluster ${CLUSTER_CONTEXT}]"
  kind delete cluster ${CLUSTER_CONTEXT}

  if [ ${SHOW_VOLCANO_LOGS} -eq 1 ]; then
    echo "===================================================================================="
    echo "=============================>>>>> Scheduler Logs <<<<<============================="
    echo "===================================================================================="

    cat scheduler.log

    echo "===================================================================================="
    echo "=============================>>>>> Controller Logs <<<<<============================"
    echo "===================================================================================="

    cat controller.log
  fi
}

echo $* | grep -E -q "\-\-help|\-h"
if [ $? -eq 0 ]; then
  echo "Customize the kind-cluster name:

    export CLUSTER_NAME=<custom cluster name>

Customize kind options other than --name:

    export KIND_OPT=<kind options>

Disable displaying volcano component logs:

    export SHOW_VOLCANO_LOGS=0

If you don't have kindest/node:v1.13.2-huawei on the host, checkout the following url to build.

    http://code-cbu.huawei.com/CBU-PaaS/Community/K8S/kind/tags/v0.1.0-huawei
"
  exit 0
fi


trap cleanup EXIT


kind-up-cluster

KUBECONFIG="$(kind get kubeconfig-path ${CLUSTER_CONTEXT})"

install-volcano

# Run e2e test
cd ${VK_ROOT}
KUBECONFIG=${KUBECONFIG} go test ./test/e2e -v -timeout 30m
