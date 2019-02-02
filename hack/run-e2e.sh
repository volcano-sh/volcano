#!/bin/bash

export PATH="${HOME}/.kubeadm-dind-cluster:${PATH}"
export MASTER="127.0.0.1:8080"
export VK_BIN=_output/bin
export LOG_LEVEL=3
export NUM_NODES=3
export CERT_PATH=/etc/kubernetes/pki
export HOST=localhost
export HOSTPORT=32222

kubectl --server=http://${MASTER} create -f config/crds/scheduling_v1alpha1_podgroup.yaml
kubectl --server=http://${MASTER} create -f config/crds/scheduling_v1alpha1_queue.yaml
kubectl --server=http://${MASTER} create -f config/crds/batch_v1alpha1_job.yaml
kubectl --server=http://${MASTER} create -f config/crds/bus_v1alpha1_command.yaml

# config admission-controller TODO: make it easier to deploy
ca_crt=`cat ${CERT_PATH}/ca.crt | base64`
ca_crt=`echo $ca_crt | sed 's/ //g'`
sed -i "s|{{ca.crt}}|$ca_crt|g" config/admission-deploy/admission-config.yaml
sed -i "s|{{host}}|${HOST}|g" config/admission-deploy/admission-config.yaml
sed -i "s|{{hostPort}}|${HOSTPORT}|g" config/admission-deploy/admission-config.yaml

kubectl create -f config/admission-deploy/admission-config.yaml

# start controller
nohup ${VK_BIN}/vk-controllers --kubeconfig ${HOME}/.kube/config --master=${MASTER} --logtostderr --v ${LOG_LEVEL} > controller.log 2>&1 &

# start scheduler
nohup ${VK_BIN}/vk-scheduler --kubeconfig ${HOME}/.kube/config --master=${MASTER} --logtostderr --v ${LOG_LEVEL} > scheduler.log 2>&1 &

# start admission-controller
nohup ${VK_BIN}/ad-controller --tls-cert-file=${CERT_PATH}/apiserver.crt --tls-private-key-file=${CERT_PATH}/apiserver.key --kubeconfig ${HOME}/.kube/config --port ${HOSTPORT} --logtostderr --v ${LOG_LEVEL} > admission.log 2>&1 &

# clean up
function cleanup {
    killall -9 vk-scheduler vk-controller ad-controller

    echo "===================================================================================="
    echo "=============================>>>>> Scheduler Logs <<<<<============================="
    echo "===================================================================================="

    cat scheduler.log

    echo "===================================================================================="
    echo "=============================>>>>> Controller Logs <<<<<============================"
    echo "===================================================================================="

    cat controller.log

    echo "===================================================================================="
    echo "=============================>>>>> admission Logs <<<<<============================"
    echo "===================================================================================="

    cat admission.log
}

trap cleanup EXIT

# Run e2e test
go test ./test/e2e -v
