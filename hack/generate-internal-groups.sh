#!/usr/bin/env bash

# Copyright 2017 The Kubernetes Authors.
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

set -o errexit
set -o nounset
set -o pipefail

# generate-internal-groups generates everything for a project with internal types, e.g. an
# user-provided API server based on k8s.io/apiserver.

if [ "$#" -lt 5 ] || [ "${1}" == "--help" ]; then
  cat <<EOF
Usage: $(basename "$0") <generators> <output-package> <internal-apis-package> <extensiona-apis-package> <groups-versions> ...

  <generators>        the generators comma separated to run (deepcopy,defaulter,conversion,client,lister,informer,openapi) or "all".
  <output-package>    the output package name (e.g. github.com/example/project/pkg/generated).
  <int-apis-package>  the internal types dir (e.g. github.com/example/project/pkg/apis).
  <ext-apis-package>  the external types dir (e.g. github.com/example/project/pkg/apis or githubcom/example/apis).
  <groups-versions>   the groups and their versions in the format "groupA:v1,v2 groupB:v1 groupC:v2", relative
                      to <api-package>.
  ...                 arbitrary flags passed to all generator binaries.

Examples:
  $(basename "$0") all                           github.com/example/project/pkg/client github.com/example/project/pkg/apis github.com/example/project/pkg/apis "foo:v1 bar:v1alpha1,v1beta1"
  $(basename "$0") deepcopy,defaulter,conversion github.com/example/project/pkg/client github.com/example/project/pkg/apis github.com/example/project/apis     "foo:v1 bar:v1alpha1,v1beta1"
EOF
  exit 0
fi

GENS="$1"
OUTPUT_PKG="$2"
INT_APIS_PKG="$3"
EXT_APIS_PKG="$4"
GROUPS_WITH_VERSIONS="$5"
shift 5

# update-gencode.sh still passes --output-base for historical GOPATH layouts; k8s.io/code-generator
# v0.36+ (gengo/v2) binaries do not define this flag.
EXTRA_ARGS=()
while (($#)); do
  case "$1" in
    --output-base)
      if (($# >= 2)); then
        shift 2
      else
        shift
      fi
      ;;
    *)
      EXTRA_ARGS+=("$1")
      shift
      ;;
  esac
done
set -- "${EXTRA_ARGS[@]}"

CODEGEN_VERSION="${CODEGEN_VERSION:-v0.36.0}"
SCRIPT_ROOT="$(cd "$(dirname "${0}")/.." && pwd)"
CODEGEN_BINDIR="${SCRIPT_ROOT}/_output/bin"

function codegen::join() { local IFS="$1"; shift; echo "$*"; }

codegen::sync_tools() {
  mkdir -p "${CODEGEN_BINDIR}"
  # openapi-gen is not under k8s.io/code-generator since ~1.31; it lives in k8s.io/kube-openapi.
  # This script's default callers only use deepcopy/conversion; install openapi separately if needed.
  GOBIN="${CODEGEN_BINDIR}" GO111MODULE=on GOOS="${OS:-}" GOFLAGS="-mod=mod" go install \
    k8s.io/code-generator/cmd/defaulter-gen@"${CODEGEN_VERSION}" \
    k8s.io/code-generator/cmd/conversion-gen@"${CODEGEN_VERSION}" \
    k8s.io/code-generator/cmd/client-gen@"${CODEGEN_VERSION}" \
    k8s.io/code-generator/cmd/lister-gen@"${CODEGEN_VERSION}" \
    k8s.io/code-generator/cmd/informer-gen@"${CODEGEN_VERSION}" \
    k8s.io/code-generator/cmd/deepcopy-gen@"${CODEGEN_VERSION}"
}

codegen::run() {
  local cmd="$1"
  shift
  ( cd "${SCRIPT_ROOT}" && GO111MODULE=on GOFLAGS="-mod=mod" "${CODEGEN_BINDIR}/${cmd}" "$@" )
}

codegen::sync_tools

# enumerate group versions
ALL_FQ_APIS=() # e.g. k8s.io/kubernetes/pkg/apis/apps k8s.io/api/apps/v1
INT_FQ_APIS=() # e.g. k8s.io/kubernetes/pkg/apis/apps
EXT_FQ_APIS=() # e.g. k8s.io/api/apps/v1
for GVs in ${GROUPS_WITH_VERSIONS}; do
  IFS=: read -r G Vs <<<"${GVs}"

  if [ -n "${INT_APIS_PKG}" ]; then
    ALL_FQ_APIS+=("${INT_APIS_PKG}/${G}")
    INT_FQ_APIS+=("${INT_APIS_PKG}/${G}")
  fi

  # enumerate versions
  for V in ${Vs//,/ }; do
    ALL_FQ_APIS+=("${EXT_APIS_PKG}/${G}/${V}")
    EXT_FQ_APIS+=("${EXT_APIS_PKG}/${G}/${V}")
  done
done

if [ "${GENS}" = "all" ] || grep -qw "deepcopy" <<<"${GENS}"; then
  echo "Generating deepcopy funcs"
  codegen::run deepcopy-gen --output-file zz_generated.deepcopy "$@" "${ALL_FQ_APIS[@]}"
fi

if [ "${GENS}" = "all" ] || grep -qw "defaulter" <<<"${GENS}"; then
  echo "Generating defaulters"
  codegen::run defaulter-gen --output-file zz_generated.defaults "$@" "${EXT_FQ_APIS[@]}"
fi

if [ "${GENS}" = "all" ] || grep -qw "conversion" <<<"${GENS}"; then
  echo "Generating conversions"
  codegen::run conversion-gen --output-file zz_generated.conversion "$@" "${ALL_FQ_APIS[@]}"
fi

if [ "${GENS}" = "all" ] || grep -qw "client" <<<"${GENS}"; then
  echo "Generating clientset for ${GROUPS_WITH_VERSIONS} at ${OUTPUT_PKG}/${CLIENTSET_PKG_NAME:-clientset}"
  if [ -n "${INT_APIS_PKG}" ]; then
    IFS=" " read -r -a APIS <<< "$(printf '%s/ ' "${INT_FQ_APIS[@]}")"
    codegen::run client-gen --clientset-name "${CLIENTSET_NAME_INTERNAL:-internalversion}" --input-base "" --input "$(codegen::join , "${APIS[@]}")" --output-dir "${OUTPUT_PKG}" --output-pkg "${OUTPUT_PKG}/${CLIENTSET_PKG_NAME:-clientset}" "$@"
  fi
  codegen::run client-gen --clientset-name "${CLIENTSET_NAME_VERSIONED:-versioned}" --input-base "" --input "$(codegen::join , "${EXT_FQ_APIS[@]}")" --output-dir "${OUTPUT_PKG}" --output-pkg "${OUTPUT_PKG}/${CLIENTSET_PKG_NAME:-clientset}" "$@"
fi

if [ "${GENS}" = "all" ] || grep -qw "lister" <<<"${GENS}"; then
  echo "Generating listers for ${GROUPS_WITH_VERSIONS} at ${OUTPUT_PKG}/listers"
  codegen::run lister-gen --output-dir "${OUTPUT_PKG}" --output-pkg "${OUTPUT_PKG}/listers" "$@" "${ALL_FQ_APIS[@]}"
fi

if [ "${GENS}" = "all" ] || grep -qw "informer" <<<"${GENS}"; then
  echo "Generating informers for ${GROUPS_WITH_VERSIONS} at ${OUTPUT_PKG}/informers"
  codegen::run informer-gen \
    --versioned-clientset-package "${OUTPUT_PKG}/${CLIENTSET_PKG_NAME:-clientset}/${CLIENTSET_NAME_VERSIONED:-versioned}" \
    --internal-clientset-package "${OUTPUT_PKG}/${CLIENTSET_PKG_NAME:-clientset}/${CLIENTSET_NAME_INTERNAL:-internalversion}" \
    --listers-package "${OUTPUT_PKG}/listers" \
    --output-dir "${OUTPUT_PKG}" \
    --output-pkg "${OUTPUT_PKG}/informers" \
    "$@" \
    "${ALL_FQ_APIS[@]}"
fi

if [ "${GENS}" = "all" ] || grep -qw "openapi" <<<"${GENS}"; then
  echo "Generating OpenAPI definitions for ${GROUPS_WITH_VERSIONS} at ${OUTPUT_PKG}/openapi"
  declare -a OPENAPI_EXTRA_PACKAGES
  codegen::run openapi-gen \
    --output-dir "${OUTPUT_PKG}" \
    --output-pkg "${OUTPUT_PKG}/openapi" \
    --output-file zz_generated.openapi \
    "$@" \
    "${EXT_FQ_APIS[@]}" \
    "${OPENAPI_EXTRA_PACKAGES[@]+"${OPENAPI_EXTRA_PACKAGES[@]}"}" \
    k8s.io/apimachinery/pkg/apis/meta/v1 \
    k8s.io/apimachinery/pkg/runtime \
    k8s.io/apimachinery/pkg/version
fi
