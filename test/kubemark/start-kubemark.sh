#!/usr/bin/env bash

VK_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/../..
TMP_ROOT="${VK_ROOT}/vendor/k8s.io/kubernetes"
KUBE_ROOT=$(readlink -e "${TMP_ROOT}" 2> /dev/null || perl -MCwd -e 'print Cwd::abs_path shift' "${TMP_ROOT}")
KUBEMARK_DIRECTORY="${KUBE_ROOT}/test/kubemark"
RESOURCE_DIRECTORY="${KUBEMARK_DIRECTORY}/resources"
CRD_DIRECTORY="${VK_ROOT}/deployment/kube-batch/templates"
QUEUE_DIR="${VK_ROOT}/config/queue"

#Release version for kube batch
RELEASE_VER=v0.4.2

#Ensure external cluster exists and kubectl binary works
kubectl get nodes
if [[ $? != 0 ]]; then
  echo "External kubernetes cluster required for kubemark"
  exit 1
fi

#Build kubernetes Binary and copy to _output folder
if [ ! -d "$KUBE_ROOT/_output" ]; then
  #If source folder is specified, overwrite the _output folder
  if [[ "${SOURCE_OUTPUT}xxx" != "xxx" ]]; then
    echo "Copying release files into kubernetes output folder from ${SOURCE_OUTPUT}"
    cp -r ${SOURCE_OUTPUT} $KUBE_ROOT
  else
    echo "Building kubernetes in temp folder /tmp/src/k8s.io and copying release files."
    mkdir -p /tmp/src/k8s.io
    cd /tmp/src/k8s.io
    git clone https://github.com/kubernetes/kubernetes.git
    cd kubernetes
    make quick-release
    mv _output/  $KUBE_ROOT
  fi
fi

#Update kubemark script to install kube-batch when starting master
echo "Modify kubemark master script"
cp "${KUBEMARK_DIRECTORY}/resources/start-kubemark-master.sh" "${KUBEMARK_DIRECTORY}/resources/start-kubemark-master.sh.bak"
#Appending lines to start kube-batch
src="start-kubemaster-component \"kube-scheduler\""
dest="start-kubemaster-component \"kube-scheduler\" \ncp \${KUBE_ROOT}/kubeconfig.kubemark /etc/srv/kubernetes \ncp \${KUBE_ROOT}/kube-batch.yaml /etc/kubernetes/manifests\n"
sed -i "s@${src}@${dest}@g" "${KUBEMARK_DIRECTORY}/resources/start-kubemark-master.sh"

#Appending lines to copy kube-batch.yaml
cp "${KUBEMARK_DIRECTORY}/start-kubemark.sh" "${KUBEMARK_DIRECTORY}/start-kubemark.sh.bak"
src1="\"\${SERVER_BINARY_TAR}\" \\\\"
dest1="\"\${SERVER_BINARY_TAR}\" \\\\\n    \"\${RESOURCE_DIRECTORY}/kube-batch.yaml\" \\\\"
sed -i "s@${src1}@${dest1}@g" "${KUBEMARK_DIRECTORY}/start-kubemark.sh"

#Update kube-batch yaml and copy it to resource folder
cp "${VK_ROOT}/test/kubemark/kube-batch.yaml"  ${RESOURCE_DIRECTORY}
sed -i "s@{{RELEASE_VER}}@${RELEASE_VER}@g" "${RESOURCE_DIRECTORY}/kube-batch.yaml"

#Start the cluster
bash -x ${KUBEMARK_DIRECTORY}/start-kubemark.sh

#creating the CRD Queue and PodGroup
echo "Creating kube batch resource in cluster via folder ${CRD_DIRECTORY}."
kubectl --kubeconfig="${RESOURCE_DIRECTORY}"/kubeconfig.kubemark apply -f  "${CRD_DIRECTORY}"/scheduling_v1alpha1_queue.yaml
kubectl --kubeconfig="${RESOURCE_DIRECTORY}"/kubeconfig.kubemark apply -f  "${CRD_DIRECTORY}"/scheduling_v1alpha1_podgroup.yaml

#creating default queue
kubectl --kubeconfig="${RESOURCE_DIRECTORY}"/kubeconfig.kubemark apply -f  "${QUEUE_DIR}"/default.yaml

#copy the kubemark config
cp ${RESOURCE_DIRECTORY}/kubeconfig.kubemark  "${VK_ROOT}/test/kubemark"

