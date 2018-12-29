#!/bin/bash

setup_k8s_cluster() {
	setup_k8s_cluster_dind
}

cleanup_k8s_cluster() {
	cleanup_k8s_cluster_dind
}

setup_k8s_cluster_dind() {
	export PATH="${HOME}/.kubeadm-dind-cluster:${PATH}"

	dind_url=https://cdn.rawgit.com/kubernetes-sigs/kubeadm-dind-cluster/master/fixed/dind-cluster-v1.12.sh
	dind_dest=./hack/dind-cluster-v1.12.sh

	# start k8s dind cluster
	curl ${dind_url} --output ${dind_dest}
	chmod +x ${dind_dest}
	${dind_dest} up
}

cleanup_k8s_cluster_dind() {
	./hack/dind-cluster-v1.12.sh down
}
