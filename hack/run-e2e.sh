#!/bin/bash

export PATH="${HOME}/.kubeadm-dind-cluster:${PATH}"
export MASTER="127.0.0.1:8080"
export VK_BIN=_output/bin
export LOG_LEVEL=3
export NUM_NODES=3

kubectl --server=http://${MASTER} create -f config/crds/scheduling_v1alpha1_podgroup.yaml
kubectl --server=http://${MASTER} create -f config/crds/scheduling_v1alpha1_queue.yaml
kubectl --server=http://${MASTER} create -f config/crds/batch_v1alpha1_job.yaml
kubectl --server=http://${MASTER} create -f config/crds/bus_v1alpha1_command.yaml

# start controller
nohup ${VK_BIN}/vk-controllers --kubeconfig ${HOME}/.kube/config --master=${MASTER} --logtostderr --v ${LOG_LEVEL} > controller.log 2>&1 &

# start scheduler
nohup ${VK_BIN}/vk-scheduler --kubeconfig ${HOME}/.kube/config --master=${MASTER} --logtostderr --v ${LOG_LEVEL} > scheduler.log 2>&1 &

# clean up
function cleanup {
    killall -9 vk-scheduler vk-controller

    echo "===================================================================================="
    echo "=============================>>>>> Scheduler Logs <<<<<============================="
    echo "===================================================================================="

    cat scheduler.log

    echo "===================================================================================="
    echo "=============================>>>>> Controller Logs <<<<<============================"
    echo "===================================================================================="

    cat controller.log
}

trap cleanup EXIT

# Run e2e test
go test ./test/e2e -v
