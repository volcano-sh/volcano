#!/bin/bash

export PATH="${HOME}/.kubeadm-dind-cluster:${PATH}"
export KA_BIN=_output/bin
export LOG_LEVEL=3
export NUM_NODES=3

dind_url=https://cdn.rawgit.com/kubernetes-sigs/kubeadm-dind-cluster/master/fixed/dind-cluster-v1.13.sh
dind_dest=./hack/dind-cluster-v1.13.sh

# start k8s dind cluster
curl ${dind_url} --output ${dind_dest}
chmod +x ${dind_dest}
${dind_dest} up

kubectl create -f deployment/kube-batch/templates/scheduling_v1alpha1_podgroup.yaml
kubectl create -f deployment/kube-batch/templates/scheduling_v1alpha1_queue.yaml
kubectl create -f deployment/kube-batch/templates/default.yaml

# start kube-batch
nohup ${KA_BIN}/kube-batch --kubeconfig ${HOME}/.kube/config --scheduler-conf=config/kube-batch-conf.yaml --logtostderr --v ${LOG_LEVEL} > scheduler.log 2>&1 &

# clean up
function cleanup {
    killall -9 kube-batch
    ./hack/dind-cluster-v1.13.sh down

    echo "===================================================================================="
    echo "=============================>>>>> Scheduler Logs <<<<<============================="
    echo "===================================================================================="

    cat scheduler.log
}

trap cleanup EXIT

# Run e2e test
go test ./test/e2e -v -timeout 30m
