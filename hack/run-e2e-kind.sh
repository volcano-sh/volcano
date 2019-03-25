#!/bin/bash

export VK_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
export VK_BIN=${VK_ROOT}/_output/bin
export LOG_LEVEL=3
export SHOW_VOLCANO_LOGS=${SHOW_VOLCANO_LOGS:-1}
export CLEANUP_CLUSTER=${CLEANUP_CLUSTER:-1}

if [[ "${CLUSTER_NAME}xxx" != "xxx" ]];then
  export CLUSTER_CONTEXT="--name ${CLUSTER_NAME}"
else
  export CLUSTER_CONTEXT="--name integration"
fi

export KIND_OPT=${KIND_OPT:=" --config ${VK_ROOT}/hack/e2e-kind-config.yaml"}

# check if kind installed
function check-prerequisites {
  echo "checking prerequisites"
  which kind >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo "kind not installed, exiting."
    exit 1
  else
    echo -n "found kind, version: " && kind version
  fi

  which kubectl >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo "kubectl not installed, exiting."
    exit 1
  else
    echo -n "found kubectl, " && kubectl version --short --client
  fi
}

# spin up cluster with kind command
function kind-up-cluster {
  check-prerequisites
  echo "Running kind: [kind create cluster ${CLUSTER_CONTEXT} ${KIND_OPT}]"
  kind create cluster ${CLUSTER_CONTEXT} ${KIND_OPT}
}

function install-volcano {
  echo "Preparing helm tiller service account"
  kubectl create serviceaccount --namespace kube-system tiller
  kubectl create clusterrolebinding tiller-cluster-rule --clusterrole=cluster-admin --serviceaccount=kube-system:tiller

  echo "Install helm via script and waiting tiller becomes ready"
  curl https://raw.githubusercontent.com/helm/helm/master/scripts/get > get_helm.sh
  #TODO: There are some issue with helm's latest version, remove '--version' when it get fixed.
  chmod 700 get_helm.sh && ./get_helm.sh   --version v2.13.0
  helm init --service-account tiller --kubeconfig ${KUBECONFIG} --wait

  echo "Loading docker images into kind cluster"
  kind load docker-image ${IMAGE}-controllers:${TAG}  ${CLUSTER_CONTEXT}
  kind load docker-image ${IMAGE}-scheduler:${TAG}  ${CLUSTER_CONTEXT}
  kind load docker-image ${IMAGE}-admission:${TAG}  ${CLUSTER_CONTEXT}

  echo "Install volcano plugin into cluster...."
  helm plugin install --kubeconfig ${KUBECONFIG} installer/chart/volcano/plugins/gen-admission-secret
  helm gen-admission-secret --service integration-admission-service --namespace kube-system

  echo "Install volcano chart"
  helm install installer/chart/volcano --namespace kube-system --name integration --kubeconfig ${KUBECONFIG}
}

function uninstall-volcano {
  helm delete integration --purge --kubeconfig ${KUBECONFIG}
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


kind-up-cluster

export KUBECONFIG="$(kind get kubeconfig-path ${CLUSTER_CONTEXT})"

install-volcano

# Run e2e test
cd ${VK_ROOT}
KUBECONFIG=${KUBECONFIG} go test ./test/e2e -v -timeout 30m
