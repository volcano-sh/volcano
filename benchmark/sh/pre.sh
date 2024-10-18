#!/bin/bash
set -e

if [ $# -eq 0 ]; then
  echo "Error: Please provide the number of nodes to create."
  echo "Usage: $0 <number_of_nodes>"
  exit 1
fi


if ! kind get clusters | grep -q "volcano-bm"; then
  kind create cluster --name volcano-bm --config ./kind/kind-config.yaml
else
  echo "Cluster 'volcano-bm' already exists. Skipping creation."
fi

# 如果本地存在镜像，则load到kind集群
if ! docker images curlimages/curl:latest --format "{{.Repository}}:{{.Tag}}" | grep -q "curlimages/curl:latest"; then
  docker pull curlimages/curl:latest
fi
kind load docker-image curlimages/curl:latest --name volcano-bm

if ! docker images alpine:latest --format "{{.Repository}}:{{.Tag}}" | grep -q "alpine:latest"; then
  docker pull alpine:latest
fi
kind load docker-image alpine:latest --name volcano-bm


if ! docker images volcanosh/vc-webhook-manager:latest --format "{{.Repository}}:{{.Tag}}" | grep -q "volcanosh/vc-webhook-manager:latest"; then
  docker pull volcanosh/vc-webhook-manager:latest
fi
kind load docker-image volcanosh/vc-webhook-manager:latest --name volcano-bm

if ! docker images volcanosh/vc-scheduler:latest --format "{{.Repository}}:{{.Tag}}" | grep -q "volcanosh/vc-scheduler:latest"; then
  docker pull volcanosh/vc-scheduler:latest
fi
kind load docker-image volcanosh/vc-scheduler:latest --name volcano-bm

if ! docker images volcanosh/vc-controller-manager:latest --format "{{.Repository}}:{{.Tag}}" | grep -q "volcanosh/vc-controller-manager:latest"; then
  docker pull vc-controller-manager:latest
fi
kind load docker-image volcanosh/vc-controller-manager:latest --name volcano-bm

if helm list -n volcano-system | grep -q "volcano"; then
  echo "volcano Chart already installed. Skipping installation."
else
  echo "volcano Chart not found. Installing..."
  #./kind/install-volcano.sh
  helm install volcano ../../installer/helm/chart/volcano -n volcano-system --create-namespace
  echo "Volcano installed successfully"
fi

if helm list -n monitoring | grep -q "kube-prometheus-stack"; then
  echo "kube-prometheus-stack Chart already installed. Skipping installation."
else
  echo "kube-prometheus-stack Chart not found. Installing..."
  ./monitor/install.sh
  echo "kube-prometheus-stack installed successfully"
fi

echo "Start setup kwok"
./kwok/kwok-setup.sh "$1"
echo "Kwok setup successfully"