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

export KIND_OPT=${KIND_OPT:="--image kindest/node:v1.13.2-huawei --config ${VK_ROOT}/hack/e2e-kind-config.yaml"}

export KIND_IMAGE=$(echo ${KIND_OPT} |grep -E -o "image \w+\/[^ ]*" | sed "s/image //")

export IMAGE=${IMAGE:-volcano}
export TAG=${TAG:-0.1}
export TILLE_IMAGE=${TILLE_IMAGE:-"gcr.io/kubernetes-helm/tiller:v2.11.0"}
export WEAVE_NET_IMAGE=${WEAVE_NET_IMAGE:-"weaveworks/weave-npc:2.5.1"}
export WEAVE_KUBE_IMAGE=${WEAVE_KUBE_IMAGE:-"weaveworks/weave-kube:2.5.1"}
export TEST_NGINX_IMAGE=${TEST_NGINX_IMAGE:-"nginx:1.14"}
export TEST_BUSYBOX_IMAGE=${TEST_BUSYBOX_IMAGE:-"busybox:1.24"}

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

  which helm >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo "helm not installed, exiting."
    exit 1
  else
    echo -n "found helm, " && helm version -c --short
  fi

  which go >/dev/null 2>&1
  if [[ $? -ne 0 ]]; then
    echo "go not installed, exiting."
    exit 1
  else
    echo -n "found go, " && go version
  fi
}

# check if the images that kind use exists.
function check-kind-image {
  check-image ${KIND_IMAGE}
}

function check-image {
  image=$1
  docker images | awk '{print $1":"$2}' | grep -q "${image}"
  if [ $? -ne 0 ]; then
    echo "image: ${image} not found."
    exit 1
  fi
  echo found image: ${image}
}

function check-all-image {
  # used for cluster install
  check-kind-image
  check-image ${WEAVE_NET_IMAGE}
  check-image ${WEAVE_KUBE_IMAGE}
  # used for volcano install
  check-image ${TILLE_IMAGE}
  check-image ${IMAGE}-controllers:${TAG}
  check-image ${IMAGE}-scheduler:${TAG}
  check-image ${IMAGE}-admission:${TAG}
  # used for volcano test
  check-image ${TEST_BUSYBOX_IMAGE}
  check-image ${TEST_NGINX_IMAGE}
}
check-all-image

# spin up cluster with kind command
function kind-up-cluster {
  check-prerequisites

  echo "Running kind: [kind create cluster ${CLUSTER_CONTEXT} ${KIND_OPT}]"
  kind create cluster ${CLUSTER_CONTEXT} ${KIND_OPT}
  kind load docker-image ${WEAVE_NET_IMAGE} ${CLUSTER_CONTEXT}
  kind load docker-image ${WEAVE_KUBE_IMAGE} ${CLUSTER_CONTEXT}
}

function install-volcano {
  echo "Checking required image"

  echo "Preparing helm tiller service account"
  kubectl create serviceaccount --namespace kube-system tiller
  kubectl create clusterrolebinding tiller-cluster-rule --clusterrole=cluster-admin --serviceaccount=kube-system:tiller

  #echo "Install helm via script and waiting tiller becomes ready"
  #curl --insecure https://raw.githubusercontent.com/helm/helm/master/scripts/get > get_helm.sh
  #TODO: There are some issue with helm's latest version, remove '--version' when it get fixed.
  #chmod 700 get_helm.sh && ./get_helm.sh   --version v2.13.0
  echo "Install tiller into cluster and waiting tiller becomes ready"
  kind load docker-image ${TILLE_IMAGE} ${CLUSTER_CONTEXT}
  helm init --skip-refresh --service-account tiller --kubeconfig ${KUBECONFIG} --wait

  echo "Loading docker images into kind cluster"
  kind load docker-image ${IMAGE}-controllers:${TAG}  ${CLUSTER_CONTEXT}
  kind load docker-image ${IMAGE}-scheduler:${TAG}  ${CLUSTER_CONTEXT}
  kind load docker-image ${IMAGE}-admission:${TAG}  ${CLUSTER_CONTEXT}

  echo "Install volcano plugin into cluster...."
  helm plugin remove gen-admission-secret
  helm plugin install --kubeconfig ${KUBECONFIG} installer/chart/volcano/plugins/gen-admission-secret
  helm gen-admission-secret --service integration-admission-service --namespace kube-system

  echo "Install volcano chart"
  helm install installer/chart/volcano --namespace kube-system --name integration --kubeconfig ${KUBECONFIG} --set basic.image_tag_version=${TAG}

  kind load docker-image ${TEST_BUSYBOX_IMAGE} ${CLUSTER_CONTEXT}
  kind load docker-image ${TEST_NGINX_IMAGE} ${CLUSTER_CONTEXT}
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

If you don't have kindest/node:v1.13.2-huawei on the host, checkout the following url to build.

    http://code-cbu.huawei.com/CBU-PaaS/Community/K8S/kind/tags/v0.1.0-huawei
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
KUBECONFIG=${KUBECONFIG} go test ./test/e2e -v -timeout 30m -count 1
