#!/bin/bash

export VN_BIN=_output/bin
export LOG_LEVEL=3
export NUM_NODES=3

# setup_k8s_cluster

kubectl create -f conf/crds/scheduling_v1alpha1_podgroup.yaml
kubectl create -f conf/crds/scheduling_v1alpha1_queue.yaml
kubectl create -f conf/crds/volcano_v1alph1_job.yaml

# start vn-controller
nohup ${VN_BIN}/vn-controller --kubeconfig ${HOME}/.kube/config --logtostderr --v ${LOG_LEVEL} > controller.log 2>&1 &

# start vn-scheduler
nohup ${VN_BIN}/vn-scheduler --kubeconfig ${HOME}/.kube/config --enable-namespace-as-queue=${ENABLE_NAMESPACES_AS_QUEUE} --logtostderr --v ${LOG_LEVEL} > scheduler.log 2>&1 &

# clean up
function cleanup {
	killall -9 vn-controller vn-scheduler
	# cleanup_k8s_cluster 

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

go test ./test/e2e -v


