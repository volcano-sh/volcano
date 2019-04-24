#!/usr/bin/env bash

TMP_ROOT="$(dirname "${BASH_SOURCE}")/../../vendor/k8s.io/kubernetes"
KUBE_ROOT=$(readlink -e "${TMP_ROOT}" 2> /dev/null || perl -MCwd -e 'print Cwd::abs_path shift' "${TMP_ROOT}")
KUBEMARK_DIRECTORY="${KUBE_ROOT}/test/kubemark"
RESOURCE_DIRECTORY="${KUBEMARK_DIRECTORY}/resources"

bash -x ${KUBEMARK_DIRECTORY}/stop-kubemark.sh

if [[ -f "${KUBEMARK_DIRECTORY}/resources/start-kubemark-master.sh.bak" ]]; then
    mv "${KUBEMARK_DIRECTORY}/resources/start-kubemark-master.sh.bak" "${KUBEMARK_DIRECTORY}/resources/start-kubemark-master.sh"
fi

if [[ -f "${KUBEMARK_DIRECTORY}/start-kubemark.sh.bak" ]]; then
    mv "${KUBEMARK_DIRECTORY}/start-kubemark.sh.bak" "${KUBEMARK_DIRECTORY}/start-kubemark.sh"
fi
rm -rf ${RESOURCE_DIRECTORY}/kube-batch.yaml
rm -rf /tmp/src/k8s.io
