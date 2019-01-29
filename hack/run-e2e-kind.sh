#!/bin/bash

export VK_ROOT=$(dirname "${BASH_SOURCE}")/..
export VK_BIN=${VK_ROOT}/_output/bin
export LOG_LEVEL=3
export NUM_NODES=3


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
  echo "Running kind: [kind create cluster ${CLUSTER_CONTEXT} ${KIND_ARGS}]"
  kind create cluster ${CLUSTER_CONTEXT} ${KIND_ARGS}
}

function install-volcano {
  kubectl create -f ${VK_ROOT}/config/crds/scheduling_v1alpha1_podgroup.yaml
  kubectl create -f ${VK_ROOT}/config/crds/scheduling_v1alpha1_queue.yaml
  kubectl create -f ${VK_ROOT}/config/crds/batch_v1alpha1_job.yaml
  kubectl create -f ${VK_ROOT}/config/crds/bus_v1alpha1_command.yaml

  # TODO: make vk-controllers and vk-scheduler run in container / in k8s
  # start controller
  nohup ${VK_BIN}/vk-controllers --kubeconfig ${KUBECONFIG} --logtostderr --v ${LOG_LEVEL} > controller.log 2>&1 &
  echo $! > vk-controllers.pid

  # start scheduler
  nohup ${VK_BIN}/vk-scheduler --kubeconfig ${KUBECONFIG} --logtostderr --v ${LOG_LEVEL} > scheduler.log 2>&1 &
  echo $! > vk-scheduler.pid
}

function uninstall-volcano {
  kubectl delete -f ${VK_ROOT}/config/crds/scheduling_v1alpha1_podgroup.yaml
  kubectl delete -f ${VK_ROOT}/config/crds/scheduling_v1alpha1_queue.yaml
  kubectl delete -f ${VK_ROOT}/config/crds/batch_v1alpha1_job.yaml
  kubectl delete -f ${VK_ROOT}/config/crds/bus_v1alpha1_command.yaml

  kill -9 $(cat vk-controllers.pid)
  kill -9 $(cat vk-scheduler.pid)
  rm vk-controllers.pid vk-scheduler.pid
}

# clean up
function cleanup {
  uninstall-volcano

  echo "Running kind: [kind delete cluster ${CLUSTER_CONTEXT}]"
  kind delete cluster ${CLUSTER_CONTEXT}
  
  #echo "===================================================================================="
  #echo "=============================>>>>> Scheduler Logs <<<<<============================="
  #echo "===================================================================================="

  #cat scheduler.log

  #echo "===================================================================================="
  #echo "=============================>>>>> Controller Logs <<<<<============================"
  #echo "===================================================================================="

  #cat controller.log
}

trap cleanup EXIT


kind-up-cluster

export KUBECONFIG="$(kind get kubeconfig-path ${CLUSTER_CONTEXT})"

# TODO: remove dependency of ${HOME}/.kube/config, make e2e read from $KUBECONFIG env
cp ${KUBECONFIG} ${HOME}/.kube/config

install-volcano

# Run e2e test
go test ${VK_ROOT}/test/e2e -v
