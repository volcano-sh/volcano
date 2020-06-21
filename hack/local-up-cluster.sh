#!/bin/bash

# Copyright 2020 The Volcano Authors.

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

if [ -z $GOPATH ]; then
    echo "Please set GOPATH to start the cluster :)"
    exit 1
fi

K8S_HOME=$GOPATH/src/k8s.io/kubernetes
VC_HOME=$GOPATH/src/volcano.sh/volcano

CERT_DIR=${VC_HOME}/volcano/certs

LOCALHOST="127.0.0.1"
API_PORT="6443"

ROOT_CA=
ROOT_CA_KEY=

SERVICE_ACCOUNT_KEY=${VC_HOME}/volcano/certs/service-account.key

function install_tools {
    for d in work logs certs config static-pods
    do
        mkdir -p ${VC_HOME}/volcano/$d
    done

    go get -u github.com/cloudflare/cfssl/cmd/...
}

function build_binaries {
    echo "Building Kubernetes ...... "
    echo "$(
        cd $K8S_HOME
        make kubectl kube-controller-manager kube-apiserver kubelet kube-proxy
    )"

    echo "Building Volcano ...... "
    echo "$(
        cd $VC_HOME
        make
    )"
}

function create_certkey {
    local name=$1
    local cn=$2
    local org=$3

    local hosts=""
    local SEP=""

    shift 3
    while [ -n "${1:-}" ]; do
        hosts+="${SEP}\"$1\""
        SEP=","
        shift 1
    done

    echo '{"CN":"'${cn}'","hosts":['${hosts}'],"key":{"algo":"rsa","size":2048},"names":[{"O":"'${org}'"}]}' \
        | cfssl gencert -ca=${CERT_DIR}/root.pem -ca-key=${CERT_DIR}/root-key.pem -config=${CERT_DIR}/root-ca-config.json - \
        | cfssljson -bare ${CERT_DIR}/$name
}

function generate_cert_files {
    openssl genrsa -out "${SERVICE_ACCOUNT_KEY}" 2048 2>/dev/null

    echo '{"signing":{"default":{"expiry":"8760h","usages":["signing","key encipherment","server auth","client auth"]}}}' \
        > ${CERT_DIR}/root-ca-config.json

    echo '{"CN":"volcano","key":{"algo":"rsa","size":2048},"names":[{"O":"volcano"}]}' | cfssl gencert -initca - \
        | cfssljson -bare ${CERT_DIR}/root

    create_certkey "kube-apiserver" "kubernetes.default" "volcano" "kubernetes.default.svc" "localhost" "127.0.0.1" "10.0.0.1"
    create_certkey "admin" "system:admin" "system:masters"
    create_certkey "kube-proxy" "system:kube-proxy" "volcano"
    create_certkey "kubelet" "system:node:127.0.0.1" "system:nodes"
    create_certkey "controller-manager" "system:kube-controller-manager" "volcano"
    create_certkey "scheduler" "system:scheduler" "volcano"
    create_certkey "webhook-manager" "volcano-webhook-manager" "volcano" "localhost" "127.0.0.1"

    write_kube_config "controller-manager"
    write_kube_config "scheduler"
    write_kube_config "kubelet"
    write_kube_config "admin"
}

function write_kube_config {
    local name=$1

    kubectl config set-cluster local --server=https://${LOCALHOST}:6443 --certificate-authority=${CERT_DIR}/root.pem \
            --kubeconfig ${VC_HOME}/volcano/config/${name}.config

    kubectl config set-credentials myself --client-key=${CERT_DIR}/${name}-key.pem \
            --client-certificate=${CERT_DIR}/${name}.pem --kubeconfig ${VC_HOME}/volcano/config/${name}.config

    kubectl config set-context local --cluster=local --user=myself --kubeconfig ${VC_HOME}/volcano/config/${name}.config
    kubectl config use-context local --kubeconfig ${VC_HOME}/volcano/config/${name}.config

    # kubectl --kubeconfig ./controller-manager.config config view --minify --flatten > ${TOP_DIR}/volcano/config/controller-manager.config
}

function start_etcd {
    nohup ${K8S_HOME}/third_party/etcd/etcd \
        --advertise-client-urls="http://${LOCALHOST}:2379" \
        --listen-client-urls="http://0.0.0.0:2379" \
        --data-dir=${VC_HOME}/volcano/work/etcd \
        --debug > ${VC_HOME}/volcano/logs/etcd.log 2>&1 &
}

function start_apiserver {
    nohup ${K8S_HOME}/_output/bin/kube-apiserver \
        --logtostderr="false" \
        --log-file=${VC_HOME}/volcano/logs/kube-apiserver.log \
        --service-account-key-file=${SERVICE_ACCOUNT_KEY} \
        --etcd-servers="http://${LOCALHOST}:2379" \
        --cert-dir=${CERT_DIR} \
        --tls-cert-file=${CERT_DIR}/kube-apiserver.pem \
        --tls-private-key-file=${CERT_DIR}/kube-apiserver-key.pem \
        --client-ca-file=${CERT_DIR}/root.pem \
        --kubelet-client-certificate=${CERT_DIR}/kube-apiserver.pem \
        --kubelet-client-key=${CERT_DIR}/kube-apiserver-key.pem \
        --insecure-bind-address=0.0.0.0 \
        --secure-port=${API_PORT} \
        --storage-backend=etcd3 \
        --feature-gates=AllAlpha=false \
        --service-cluster-ip-range=10.0.0.0/24 &
}

function start_controller_manager {
    nohup ${VC_HOME}/_output/bin/vc-controller-manager \
        --v=3 \
        --logtostderr=false \
        --log-file=${VC_HOME}/volcano/logs/vc-controller-manager.log \
        --scheduler-name=default-scheduler \
        --kubeconfig=${VC_HOME}/volcano/config/controller-manager.config &

    nohup ${K8S_HOME}/_output/bin/kube-controller-manager \
        --v=3 \
        --logtostderr="false" \
        --log-file=${VC_HOME}/volcano/logs/kube-controller-manager.log \
        --service-account-private-key-file=${SERVICE_ACCOUNT_KEY} \
        --root-ca-file=${CERT_DIR}/root.pem \
        --cluster-signing-cert-file=${CERT_DIR}/root.pem \
        --cluster-signing-key-file=${CERT_DIR}/root-key.pem \
        --enable-hostpath-provisioner=false \
        --pvclaimbinder-sync-period=15s \
        --feature-gates=AllAlpha=false \
        --kubeconfig ${VC_HOME}/volcano/config/controller-manager.config \
        --use-service-account-credentials \
        --controllers=* \
        --leader-elect=false \
        --cert-dir=${CERT_DIR} &
}

function start_kubelet {
    nohup ${K8S_HOME}/_output/bin/kubelet \
        --logtostderr="false" \
        --log-file=${VC_HOME}/volcano/logs/kubelet.log \
        --chaos-chance=0.0 \
        --container-runtime=docker \
        --hostname-override=${LOCALHOST} \
        --address=${LOCALHOST} \
        --kubeconfig ${VC_HOME}/volcano/config/kubelet.config \
        --feature-gates=AllAlpha=false \
        --cpu-cfs-quota=true \
        --enable-controller-attach-detach=true \
        --cgroups-per-qos=true \
        --cgroup-driver=cgroupfs \
        --eviction-hard='memory.available<100Mi,nodefs.available<10%,nodefs.inodesFree<5%' \
        --eviction-pressure-transition-period=1m \
        --pod-manifest-path=${VC_HOME}/volcano/static-pods \
        --fail-swap-on=false \
        --authorization-mode=Webhook \
        --authentication-token-webhook \
        --client-ca-file=${CERT_DIR}/root.pem \
        --cluster-dns=10.0.0.10 \
        --cluster-domain=cluster.local \
        --runtime-request-timeout=2m \
        --port=10250 &
}

function start_volcano_scheduler {
    nohup ${VC_HOME}/_output/bin/vc-scheduler \
        --v=4 \
        --logtostderr=false \
        --listen-address=":8090" \
        --log-file=${VC_HOME}/volcano/logs/vc-scheduler.log \
        --scheduler-name=default-scheduler \
        --kubeconfig=${VC_HOME}/volcano/config/scheduler.config &
}

function start_volcano_admission {
	nohup ${VC_HOME}/_output/bin/vc-webhook-manager \
		-v 3 \
        --logtostderr=false \
        --log-file=${VC_HOME}/volcano/logs/vc-webhook-manager.log \
		--ca-cert-file ${CERT_DIR}/root.pem \
        --scheduler-name=default-scheduler \
		--kubeconfig ${VC_HOME}/volcano/config/admin.config \
		--tls-cert-file ${CERT_DIR}/webhook-manager.pem \
		--tls-private-key-file ${CERT_DIR}/webhook-manager-key.pem \
		--webhook-url https://127.0.0.1:443 &
}

function cleanup_cluster {
    killall -9 etcd kube-apiserver kube-controller-manager kubelet vc-controller-manager vc-scheduler vc-webhook-manager
    rm -rf ${VC_HOME}/volcano

    # Waiting for TIME_WAIT
    sleep 6
}

function apply_volcano_crds {
    kubectl get ns --kubeconfig ${VC_HOME}/volcano/config/admin.config

    for crd in scheduling_v1beta1_podgroup.yaml scheduling_v1beta1_queue.yaml bus_v1alpha1_command.yaml batch_v1alpha1_job.yaml
    do
        kubectl apply -f ${VC_HOME}/installer/helm/chart/volcano/templates/$crd --kubeconfig ${VC_HOME}/volcano/config/admin.config
    done
}

cleanup_cluster

install_tools

# build_binaries

generate_cert_files

start_etcd
start_apiserver
apply_volcano_crds
start_controller_manager
start_volcano_admission
start_volcano_scheduler
start_kubelet


