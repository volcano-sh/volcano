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

# generate-groups generates everything for a project with external types only, e.g. a project based
# on CustomResourceDefinitions.

if [ "$#" -lt 4 ] || [ "${1}" == "--help" ]; then
  cat <<EOF
Usage: $(basename "$0") <generators> <output-package> <apis-package> <groups-versions> ...

  <generators>        the generators comma separated to run (deepcopy,defaulter,client,lister,informer) or "all".
  <output-package>    the output package name (e.g. github.com/example/project/pkg/generated).
  <apis-package>      the external types dir (e.g. github.com/example/api or github.com/example/project/pkg/apis).
  <groups-versions>   the groups and their versions in the format "groupA:v1,v2 groupB:v1 groupC:v2", relative
                      to <api-package>.
  ...                 arbitrary flags passed to all generator binaries.


Examples:
  $(basename "$0") all             github.com/example/project/pkg/client github.com/example/project/pkg/apis "foo:v1 bar:v1alpha1,v1beta1"
  $(basename "$0") deepcopy,client github.com/example/project/pkg/client github.com/example/project/pkg/apis "foo:v1 bar:v1alpha1,v1beta1"
EOF
  exit 0
fi

GENS="$1"
OUTPUT_PKG="$2"
APIS_PKG="$3"
GROUPS_WITH_VERSIONS="$4"
shift 4

# Pin to the same k8s.io/code-generator minor as volcano go.mod.
# v0.36+ generators take API packages as positional args (pflag.Args()), not --bounding-dirs/--input-dirs.
# Install binaries into _output/bin with GOFLAGS=-mod=mod: "go run" from the main module with vendor
# can mis-build or mis-forward argv so pflag sees no flags (everything becomes "packages").
CODEGEN_VERSION="${CODEGEN_VERSION:-v0.36.0}"
SCRIPT_ROOT="$(cd "$(dirname "${0}")/.." && pwd)"
CODEGEN_BINDIR="${SCRIPT_ROOT}/_output/bin"

function codegen::join() { local IFS="$1"; shift; echo "$*"; }

codegen::sync_tools() {
  mkdir -p "${CODEGEN_BINDIR}"
  GOBIN="${CODEGEN_BINDIR}" GO111MODULE=on GOOS="${OS:-}" GOFLAGS="-mod=mod" go install \
    k8s.io/code-generator/cmd/deepcopy-gen@"${CODEGEN_VERSION}" \
    k8s.io/code-generator/cmd/client-gen@"${CODEGEN_VERSION}" \
    k8s.io/code-generator/cmd/lister-gen@"${CODEGEN_VERSION}" \
    k8s.io/code-generator/cmd/informer-gen@"${CODEGEN_VERSION}"
}

codegen::run() {
  local cmd="$1"
  shift
  ( cd "${SCRIPT_ROOT}" && GO111MODULE=on GOFLAGS="-mod=mod" "${CODEGEN_BINDIR}/${cmd}" "$@" )
}

codegen::sync_tools

# enumerate group versions
FQ_APIS=() # e.g. k8s.io/api/apps/v1
for GVs in ${GROUPS_WITH_VERSIONS}; do
  IFS=: read -r G Vs <<<"${GVs}"

  # enumerate versions
  for V in ${Vs//,/ }; do
    FQ_APIS+=("${APIS_PKG}/${G}/${V}")
  done
done

if [ "${GENS}" = "all" ] || grep -qw "deepcopy" <<<"${GENS}"; then
  echo "Generating deepcopy funcs"
  codegen::run deepcopy-gen --output-file zz_generated.deepcopy "$@" "${FQ_APIS[@]}"
fi

if [ "${GENS}" = "all" ] || grep -qw "client" <<<"${GENS}"; then
  echo "Generating clientset for ${GROUPS_WITH_VERSIONS} at ${OUTPUT_PKG}/${CLIENTSET_PKG_NAME:-clientset}"
  codegen::run client-gen --clientset-name "${CLIENTSET_NAME_VERSIONED:-versioned}" --input-base "" --input "$(codegen::join , "${FQ_APIS[@]}")" --output-dir "${OUTPUT_PKG}" --output-pkg "${CLIENTSET_PKG_NAME:-clientset}" "$@"
fi

if [ "${GENS}" = "all" ] || grep -qw "lister" <<<"${GENS}"; then
  echo "Generating listers for ${GROUPS_WITH_VERSIONS} at ${OUTPUT_PKG}/listers"
  codegen::run lister-gen --output-dir "${OUTPUT_PKG}" --output-pkg "listers" "$@" "${FQ_APIS[@]}"
fi

if [ "${GENS}" = "all" ] || grep -qw "informer" <<<"${GENS}"; then
  echo "Generating informers for ${GROUPS_WITH_VERSIONS} at ${OUTPUT_PKG}/informers"
  codegen::run informer-gen \
    --versioned-clientset-package "${OUTPUT_PKG}/${CLIENTSET_PKG_NAME:-clientset}/${CLIENTSET_NAME_VERSIONED:-versioned}" \
    --listers-package "${OUTPUT_PKG}/listers" \
    --output-dir "${OUTPUT_PKG}" \
    --output-pkg "informers" \
    "$@" \
    "${FQ_APIS[@]}"
fi
