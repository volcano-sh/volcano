#!/bin/bash

# Copyright 2014 The Kubernetes Authors.
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

KUBE_ROOT="$(dirname "${BASH_SOURCE[0]}")/.."
GOPATH="$(go env GOPATH | awk -F ':' '{print $1}')"

# ---------- Configuration ----------
# Bump this version when upgrading golangci-lint for the project.
REQUIRED_VERSION="v2.8.0"
GOLANGCI_LINT_PKG="github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
# -----------------------------------

# install_golangci_lint installs the required version of golangci-lint via
# 'go install' and ensures $GOPATH/bin is on PATH.
install_golangci_lint() {
  echo "Installing golangci-lint ${REQUIRED_VERSION} ..."
  GOOS="${OS:-$(go env GOOS)}" go install "${GOLANGCI_LINT_PKG}@${REQUIRED_VERSION}"
  if [[ $? -ne 0 ]]; then
    echo "ERROR: golangci-lint installation failed."
    exit 1
  fi
  export PATH="${GOPATH}/bin:${PATH}"
  echo "golangci-lint ${REQUIRED_VERSION} installed successfully."
}

# check_golangci_lint verifies that golangci-lint is installed and at the
# required version. When the version does not match, the user is prompted to
# upgrade (or the upgrade happens automatically in CI).
check_golangci_lint() {
  echo "Checking golangci-lint installation ..."

  if ! command -v golangci-lint >/dev/null 2>&1; then
    echo "golangci-lint not found."
    install_golangci_lint
    return
  fi

  # Extract the installed version. golangci-lint v2 prints e.g.
  #   golangci-lint has version v2.8.0 built with go1.25.7 from ... on ...
  # while v1 prints e.g.
  #   golangci-lint has version 1.64.8 built with ...
  local raw_version
  raw_version="$(golangci-lint --version 2>/dev/null | head -1)"
  # Grab the semver token right after "version".
  local installed_version
  installed_version="$(echo "${raw_version}" | grep -oP 'version\s+\Kv?\d+\.\d+\.\d+')"
  # Normalise: ensure leading 'v'
  installed_version="v${installed_version#v}"

  if [[ "${installed_version}" == "${REQUIRED_VERSION}" ]]; then
    echo "golangci-lint ${REQUIRED_VERSION} found."
    return
  fi

  echo ""
  echo "VERSION MISMATCH"
  echo "  Installed: ${installed_version}  (${raw_version})"
  echo "  Required:  ${REQUIRED_VERSION}"
  echo ""

  # In CI or non-interactive shells, install automatically.
  if [[ "${CI:-}" == "true" ]] || [[ ! -t 0 ]]; then
    echo "Non-interactive environment detected. Installing the required version automatically ..."
    install_golangci_lint
    return
  fi

  # Interactive: ask the user.
  read -r -p "Install golangci-lint ${REQUIRED_VERSION} now? [Y/n] " answer
  case "${answer}" in
    [nN][oO]|[nN])
      echo "Aborted. Please install golangci-lint ${REQUIRED_VERSION} manually:"
      echo "  go install ${GOLANGCI_LINT_PKG}@${REQUIRED_VERSION}"
      exit 1
      ;;
    *)
      install_golangci_lint
      ;;
  esac
}

# run_golangci_lint executes golangci-lint and reports the result.
run_golangci_lint() {
  echo "Running golangci-lint ..."
  cd "${KUBE_ROOT}"
  local ret=0
  golangci-lint run -v || ret=$?
  if [[ ${ret} -eq 0 ]]; then
    echo "SUCCESS: golangci-lint verified."
  else
    echo "FAILED: golangci-lint stale."
    echo
    echo "Please review the above warnings. You can test via './hack/verify-golangci-lint.sh' or 'make lint'."
    echo "If the above warnings do not make sense, you can exempt this warning with a comment"
    echo " (if your reviewer is okay with it)."
    echo "In general please prefer to fix the error, we have already disabled specific lints"
    echo " that the project chooses to ignore."
    echo "See: https://golangci-lint.run/usage/false-positives/"
    echo
    exit 1
  fi
}

check_golangci_lint
run_golangci_lint
