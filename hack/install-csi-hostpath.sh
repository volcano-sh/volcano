#!/usr/bin/env bash

# Copyright 2026 The Volcano Authors.
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

# Install the upstream CSI hostpath deployment for the volume scheduling tests.
set -euo pipefail

CSI_HOSTPATH_VERSION="${CSI_HOSTPATH_VERSION:-v1.16.1}"
SNAPSHOTTER_VERSION="${SNAPSHOTTER_VERSION:-v8.2.0}"

workdir=$(mktemp -d)
trap 'rm -rf "${workdir}"' EXIT

git clone --depth 1 --branch "${CSI_HOSTPATH_VERSION}" \
  https://github.com/kubernetes-csi/csi-driver-host-path.git "${workdir}/driver"

# The upstream deployment includes the snapshotter sidecar and a snapshot class.
kubectl apply -k "github.com/kubernetes-csi/external-snapshotter/client/config/crd?ref=${SNAPSHOTTER_VERSION}"
kubectl wait --for=condition=Established \
  crd/volumesnapshots.snapshot.storage.k8s.io \
  crd/volumesnapshotcontents.snapshot.storage.k8s.io \
  crd/volumesnapshotclasses.snapshot.storage.k8s.io --timeout=60s

HOSTPATHPLUGIN_TAG="${CSI_HOSTPATH_VERSION}" bash "${workdir}/driver/deploy/kubernetes-latest/deploy.sh"
kubectl -n default rollout status statefulset/csi-hostpathplugin --timeout=300s
kubectl apply -f "${workdir}/driver/examples/csi-storageclass.yaml"
