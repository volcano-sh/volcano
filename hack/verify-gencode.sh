#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT=$(dirname "${BASH_SOURCE}")/..
source "${KUBE_ROOT}/hack/lib/init.sh"

kube::golang::verify_go_version

cd "${KUBE_ROOT}"

find_files() {
  find . -not \( \
      \( \
        -wholename './output' \
        -o -wholename './_output' \
        -o -wholename './_gopath' \
        -o -wholename './release' \
        -o -wholename './target' \
        -o -wholename '*/third_party/*' \
        -o -wholename '*/vendor/*' \
        -o -wholename '*/contrib/*' \
        -o -wholename './staging/src/k8s.io/client-go/*vendor/*' \
      \) -prune \
    \) -name '*.go'
}

diff=$(find_files | xargs git diff 2>&1) || true
if [[ -n "${diff}" ]]; then
  echo "${diff}"
  exit 1
fi
