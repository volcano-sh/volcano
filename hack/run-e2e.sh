#!/bin/bash

export PATH="${HOME}/.kubeadm-dind-cluster:${PATH}"
export KA_BIN=_output/bin
export LOG_LEVEL=3

if [ $(echo $RANDOM%2 | bc) -eq 1 ]
then
    enable_namespace_as_queue=true
else
    enable_namespace_as_queue=false
fi

export ENABLE_NAMESPACES_AS_QUEUE=$enable_namespace_as_queue

# start k8s dind cluster
./hack/dind-cluster-v1.11.sh up

kubectl create -f config/crds/scheduling_v1alpha1_podgroup.yaml
kubectl create -f config/crds/scheduling_v1alpha1_queue.yaml

# start kube-arbitrator
nohup ${KA_BIN}/kube-batchd --kubeconfig ${HOME}/.kube/config --enable-namespace-as-queue=${ENABLE_NAMESPACES_AS_QUEUE} --logtostderr --v ${LOG_LEVEL} > scheduler.log 2>&1 &

# clean up
function cleanup {
    killall -9 kube-batchd
    ./hack/dind-cluster-v1.11.sh down

    echo "===================================================================================="
    echo "=============================>>>>> Scheduler Logs <<<<<============================="
    echo "===================================================================================="

    cat scheduler.log
}

trap cleanup EXIT

# Run e2e test
go test ./test -v
