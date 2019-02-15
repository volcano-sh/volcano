#!/bin/bash

export PATH="${HOME}/.kubeadm-dind-cluster:${PATH}"
export MASTER="http://127.0.0.1:8080"
export VK_BIN=_output/bin
export LOG_LEVEL=2
export NUM_NODES=3
export CERT_PATH=/etc/kubernetes/pki
export HOST=localhost
export HOSTPORT=32222

kubectl --server=${MASTER} apply -f config/crds/scheduling_v1alpha1_podgroup.yaml
kubectl --server=${MASTER} apply -f config/crds/scheduling_v1alpha1_queue.yaml
kubectl --server=${MASTER} apply -f config/crds/batch_v1alpha1_job.yaml
kubectl --server=${MASTER} apply -f config/crds/bus_v1alpha1_command.yaml

# config admission-controller TODO: make it easier to deploy
CA_BUNDLE=`kubectl get configmap -n kube-system extension-apiserver-authentication -o=jsonpath='{.data.client-ca-file}' | base64 | tr -d '\n'`
sed -i "s|{{CA_BUNDLE}}|$CA_BUNDLE|g" hack/e2e-admission-config.yaml
sed -i "s|{{host}}|${HOST}|g" hack/e2e-admission-config.yaml
sed -i "s|{{hostPort}}|${HOSTPORT}|g" hack/e2e-admission-config.yaml

kubectl create -f hack/e2e-admission-config.yaml

# start controller
nohup ${VK_BIN}/vk-controllers --kubeconfig ${HOME}/.kube/config --master=${MASTER} --logtostderr --v ${LOG_LEVEL} > controller.log 2>&1 &

# start scheduler
nohup ${VK_BIN}/vk-scheduler --kubeconfig ${HOME}/.kube/config --master=${MASTER} --logtostderr --v ${LOG_LEVEL} > scheduler.log 2>&1 &

# start admission-controller
nohup ${VK_BIN}/ad-controller --tls-cert-file=${CERT_PATH}/apiserver.crt --tls-private-key-file=${CERT_PATH}/apiserver.key --kubeconfig ${HOME}/.kube/config --port ${HOSTPORT} --logtostderr --v ${LOG_LEVEL} > admission.log 2>&1 &

# clean up
function cleanup {
    killall -9 -r vk-scheduler -r vk-controllers -r ad-controller

    if [[ -f scheduler.log ]] ; then
        echo "===================================================================================="
        echo "=============================>>>>> Scheduler Logs <<<<<============================="
        echo "===================================================================================="
        cat scheduler.log
    fi

    if [[ -f controller.log ]] ; then
        echo "===================================================================================="
        echo "=============================>>>>> Controller Logs <<<<<============================"
        echo "===================================================================================="
        cat controller.log
    fi

    echo "===================================================================================="
    echo "=============================>>>>> admission Logs <<<<<============================"
    echo "===================================================================================="

    cat admission.log
}

trap cleanup EXIT

# Run e2e test
go test ./test/e2e -v
