#!/bin/bash
# Copyright 2019 The Volcano Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
source base.sh
set -e

# 初始化变量
node_cnt=""
volcano_v=""
use_mini_volcano=""

# 打印帮助信息
usage() {
  echo "Usage: $0 --node_cnt=<node_count> --volcano_v=<volcano_version> --use_mini_volcano=<true|false>"
  exit 1
}

# 处理输入参数
for arg in "$@"; do
  case $arg in
    --node_cnt=*)
      node_cnt="${arg#*=}"
      shift
      ;;
    --volcano_v=*)
      volcano_v="${arg#*=}"
      shift
      ;;
    --use_mini_volcano=*)
      use_mini_volcano="${arg#*=}"
      shift
      ;;
    *)
      echo "Unknown option: $arg"
      usage
      ;;
  esac
done

# 检查参数是否为空
if [[ -z "$node_cnt" || -z "$volcano_v" ]]; then
  echo "Error: Missing required parameters."
  usage
fi

# 输出参数信息
echo "Node count: $node_cnt"
echo "Volcano version: $volcano_v"

if ! kind get clusters | grep -q "volcano-bm"; then
  kind create cluster --name volcano-bm --config ./kind/kind-config.yaml
else
  echo "Cluster 'volcano-bm' already exists. Skipping creation."
fi

# 如果本地存在镜像，则load到kind集群
load_image_to_kind "curlimages/curl:latest" "volcano-bm"
load_image_to_kind "alpine:latest" "volcano-bm"
load_image_to_kind "volcanosh/vc-webhook-manager:$volcano_v" "volcano-bm"
load_image_to_kind "volcanosh/vc-scheduler:$volcano_v" "volcano-bm"
load_image_to_kind "volcanosh/vc-controller-manager:$volcano_v" "volcano-bm"
load_image_to_kind "registry.k8s.io/ingress-nginx/kube-webhook-certgen:v20221220-controller-v1.5.1-58-g787ea74b6" "volcano-bm"
load_image_to_kind "registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.13.0" "volcano-bm"
load_image_to_kind "quay.io/kiwigrid/k8s-sidecar:1.28.0" "volcano-bm"
load_image_to_kind "docker.io/grafana/grafana:11.2.2-security-01" "volcano-bm"
load_image_to_kind "quay.io/prometheus/prometheus:v2.55.0" "volcano-bm"

if helm list -n volcano-system | grep -q "volcano"; then
  echo "volcano Chart already installed. Skipping installation."
else
  echo "volcano Chart not found. Installing..."

  if [[ "$use_mini_volcano" == "true" ]]; then
    echo "Use mini volcano"
    helm install volcano --set basic.image_pull_policy="IfNotPresent" --set basic.image_tag_version="$volcano_v" \
     --set custom.controller_kube_api_qps=3000 --set custom.controller_kube_api_burst=3000 --set custom.scheduler_kube_api_qps=10000 --set custom.scheduler_kube_api_burst=10000 \
     --set custom.controller_metrics_enable=false --set custom.scheduler_node_worker_threads=200 --set custom.scheduler_schedule_period=100ms \
     --set custom.controller_worker_threads=30 --set custom.controller_worker_threads_for_gc=10 --set custom.controller_worker_threads_for_podgroup=50 \
     --set-file custom.scheduler_config_override=./custom_scheduler_config.yaml ../../installer/helm/chart/volcano -n volcano-system --create-namespace
  else
    helm install volcano --set basic.image_pull_policy="IfNotPresent" --set basic.image_tag_version="$volcano_v" \
     --set custom.controller_kube_api_qps=3000 --set custom.controller_kube_api_burst=3000 --set custom.scheduler_kube_api_qps=10000 --set custom.scheduler_kube_api_burst=10000 \
     --set custom.scheduler_node_worker_threads=200 --set custom.scheduler_schedule_period=100ms \
     --set custom.controller_worker_threads=30 --set custom.controller_worker_threads_for_gc=10 --set custom.controller_worker_threads_for_podgroup=50 \
     --set custom.controller_metrics_enable=false ../../installer/helm/chart/volcano -n volcano-system --create-namespace
  fi


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
./kwok/kwok-setup.sh "$node_cnt"
echo "Kwok setup successfully"