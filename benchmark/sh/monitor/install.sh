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

helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# 可以自定义制定镜像
#helm install kube-prometheus-stack --set alertmanager.enabled=false \
# --set prometheusOperator.admissionWebhooks.patch.image.registry="docker.io",prometheusOperator.admissionWebhooks.patch.image.repository=15841721425/kube-webhook-certgen \
# --set kube-state-metrics.image.registry="docker.io",kube-state-metrics.image.repository=bitnami/kube-state-metrics,kube-state-metrics.image.tag=2.10.1 \
# --set kube-state-metrics.prometheus.monitor.interval="1s" --set prometheus-node-exporter.prometheus.monitor.interval="1s" \
# --set prometheus.prometheusSpec.scrapeInterval="2s" \
# prometheus-community/kube-prometheus-stack -n monitoring --create-namespace

helm install kube-prometheus-stack --set alertmanager.enabled=false \
 --set kube-state-metrics.prometheus.monitor.interval="1s" --set prometheus-node-exporter.prometheus.monitor.interval="1s" \
 --set prometheus.prometheusSpec.scrapeInterval="2s" \
 prometheus-community/kube-prometheus-stack -n monitoring --create-namespace