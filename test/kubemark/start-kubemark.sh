#!/usr/bin/env bash

TMP_ROOT="$(dirname "${BASH_SOURCE}")/../../vendor/k8s.io/kubernetes"
KUBE_ROOT=$(readlink -e "${TMP_ROOT}" 2> /dev/null || perl -MCwd -e 'print Cwd::abs_path shift' "${TMP_ROOT}")
KUBEMARK_DIRECTORY="${KUBE_ROOT}/test/kubemark"
RESOURCE_DIRECTORY="${KUBEMARK_DIRECTORY}/resources"
CRD_DIRECTORY="${KUBE_ROOT}/../../../deployment/kube-batch/templates"
QUEUE_DIR="${KUBE_ROOT}/../../../config/queue"

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


#Appending lines to start kube-batch
src="start-kubemaster-component \"kube-scheduler\""
dest="start-kubemaster-component \"kube-scheduler\" \ncp \${KUBE_ROOT}/kubeconfig.kubemark /etc/srv/kubernetes \nstart-kubemaster-component \"kube-batch\""
sed -i "s@${src}@${dest}@g" "${KUBEMARK_DIRECTORY}/resources/start-kubemark-master.sh"

#Appending lines to copy kube-batch.yaml
src1="\"\${SERVER_BINARY_TAR}\" \\\\"
dest1="\"\${SERVER_BINARY_TAR}\" \\\\\n    \"\${RESOURCE_DIRECTORY}/kube-batch.yaml\" \\\\"
sed -i "s@${src1}@${dest1}@g" "${KUBEMARK_DIRECTORY}/start-kubemark.sh"


cp kube-batch.yaml  ${RESOURCE_DIRECTORY}

bash -x ${KUBEMARK_DIRECTORY}/start-kubemark.sh

#creating the CRD Queue and PodGroup
echo "Creating kube batch resource in cluster via folder ${CRD_DIRECTORY}."
kubectl --kubeconfig="${RESOURCE_DIRECTORY}"/kubeconfig.kubemark apply -f  "${CRD_DIRECTORY}"/scheduling_v1alpha1_queue.yaml
kubectl --kubeconfig="${RESOURCE_DIRECTORY}"/kubeconfig.kubemark apply -f  "${CRD_DIRECTORY}"/scheduling_v1alpha1_podgroup.yaml

#creating default queue
kubectl --kubeconfig="${RESOURCE_DIRECTORY}"/kubeconfig.kubemark apply -f  "${QUEUE_DIR}"/default.yaml

#copy the kubemark config
cp ${RESOURCE_DIRECTORY}/kubeconfig.kubemark  ./

#Reverting the script changes in the vendor and tmp
data="kube-batch.yaml"
#sed -i "/${data}/d" "${KUBEMARK_DIRECTORY}/start-kubemark.sh"
data1="kube-batch"
data2="kubeconfig.kubemark"
#sed -i "/${data1}/d" "${KUBEMARK_DIRECTORY}/resources/start-kubemark-master.sh"
#sed -i "/${data2}/d" "${KUBEMARK_DIRECTORY}/resources/start-kubemark-master.sh"
#rm -rf ${RESOURCE_DIRECTORY}/kube-batch.yaml
rm -rf /tmp/src/
