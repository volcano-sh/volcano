#!/bin/bash

export PATH="${HOME}/.kubeadm-dind-cluster:${PATH}"

# start k8s dind cluster
./hack/dind-cluster-v1.11.sh up

kubectl create -f config/crds/scheduling_v1alpha1_podgroup.yaml
kubectl create -f config/crds/extensions_v1alpha1_job.yaml
kubectl create -f config/crds/scheduling_v1alpha1_queue.yaml

# start kube-arbitrator
nohup _output/bin/kar-controllers --kubeconfig ${HOME}/.kube/config --logtostderr --v 3 > controller.log 2>&1 &
nohup _output/bin/kar-scheduler --kubeconfig ${HOME}/.kube/config --logtostderr --v 3 > scheduler.log 2>&1 &

# clean up
function cleanup {
    killall -9 kar-controllers kar-scheduler
    ./hack/dind-cluster-v1.11.sh down

    echo "=================================> Controller Logs <================================="
    cat controller.log
    echo "=================================> Scheduler Logs <================================="
    cat scheduler.log
}

trap cleanup EXIT

# Run e2e test
go test ./test -v
