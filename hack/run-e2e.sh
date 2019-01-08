#!/bin/bash

export PATH="${HOME}/.kubeadm-dind-cluster:${PATH}"
export VK_BIN=_output/bin
export LOG_LEVEL=3
export NUM_NODES=3

dind_url=https://cdn.rawgit.com/kubernetes-sigs/kubeadm-dind-cluster/master/fixed/dind-cluster-v1.12.sh
dind_dest=./hack/dind-cluster-v1.12.sh

# start k8s dind cluster
curl ${dind_url} --output ${dind_dest}
chmod +x ${dind_dest}
${dind_dest} up

kubectl create -f config/crds/scheduling_v1alpha1_podgroup.yaml
kubectl create -f config/crds/scheduling_v1alpha1_queue.yaml
kubectl create -f config/crds/batch_v1alpha1_job.yaml

# start controller
nohup ${VK_BIN}/vk-controllers --kubeconfig ${HOME}/.kube/config --logtostderr --v ${LOG_LEVEL} > controller.log 2>&1 &

# start scheduler
nohup ${VK_BIN}/vk-scheduler --kubeconfig ${HOME}/.kube/config --logtostderr --v ${LOG_LEVEL} > scheduler.log 2>&1 &

# clean up
function cleanup {
    killall -9 vk-scheduler vk-controller
    ./hack/dind-cluster-v1.12.sh down

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
