#!/bin/bash

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

# Installs the CSI hostpath driver with a 1-volume-per-node limit and no attach step,
# under a name Volcano does not special-case. Used by the CSI e2e suite.

set -euo pipefail

CSI_HOSTPATH_VERSION=${CSI_HOSTPATH_VERSION:-v1.16.1}
CSI_DRIVER_NAME=${CSI_DRIVER_NAME:-hostpath.e2e.volcano.sh}
KUBECTL=${KUBECTL:-kubectl}

WORKDIR=$(mktemp -d)
trap 'rm -rf "${WORKDIR}"' EXIT

git clone --depth 1 --branch "${CSI_HOSTPATH_VERSION}" https://github.com/kubernetes-csi/csi-driver-host-path.git "${WORKDIR}/repo"
DEPLOY_DIR="${WORKDIR}/repo/deploy/kubernetes-latest"

perl -pi -e "s/hostpath\.csi\.k8s\.io/${CSI_DRIVER_NAME}/g" "${DEPLOY_DIR}"/hostpath/*.yaml
perl -pi -e 's/^(\s*)- "--drivername=.*\n/$&$1- "--maxvolumespernode=1"\n/' "${DEPLOY_DIR}/hostpath/csi-hostpath-plugin.yaml"
perl -pi -e 's/^spec:\n/spec:\n  attachRequired: false\n/' "${DEPLOY_DIR}/hostpath/csi-hostpath-driverinfo.yaml"

${KUBECTL} apply -k "github.com/kubernetes-csi/external-snapshotter/client/config/crd?ref=v8.2.0"
${KUBECTL} wait --for=condition=Established crd/volumesnapshotclasses.snapshot.storage.k8s.io --timeout=60s

(cd "${DEPLOY_DIR}" && ./deploy.sh)

${KUBECTL} apply -f - <<EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: csi-hostpath-e2e
provisioner: ${CSI_DRIVER_NAME}
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Delete
EOF

${KUBECTL} -n default rollout status statefulset/csi-hostpathplugin --timeout=300s
