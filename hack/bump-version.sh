#!/bin/bash

# Copyright 2025 The Volcano Authors.
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

REPO_ROOT="$(git rev-parse --show-toplevel)"
declare -r REPO_ROOT
cd "${REPO_ROOT}"

declare -r STARTINGBRANCH=$(git symbolic-ref --short HEAD)
declare -r REBASEMAGIC="${REPO_ROOT}/.git/rebase-apply"

RELEASE_VERSION_FILE=".release-version"
CHART_YAML="installer/helm/chart/volcano/Chart.yaml"
VALUES_YAML="installer/helm/chart/volcano/values.yaml"
DRY_RUN=${DRY_RUN:-""}
UPSTREAM_REMOTE=${UPSTREAM_REMOTE:-upstream}
FORK_REMOTE=${FORK_REMOTE:-origin}
MAIN_REPO_ORG=${MAIN_REPO_ORG:-$(git remote get-url "$UPSTREAM_REMOTE" | awk '{gsub(/http[s]:\/\/|git@/,"")}1' | awk -F'[@:./]' 'NR==1{print $3}')}
MAIN_REPO_NAME=${MAIN_REPO_NAME:-$(git remote get-url "$UPSTREAM_REMOTE" | awk '{gsub(/http[s]:\/\/|git@/,"")}1' | awk -F'[@:./]' 'NR==1{print $4}')}

# Track if we've successfully completed all operations
SUCCESS_MARKER=""
cleanbranch=""

if [[ -z ${GITHUB_USER:-} ]]; then
  echo "Please export GITHUB_USER=<your-user> (or GH organization, if that's where your fork lives)"
  exit 1
fi

# Check if gh CLI is available
if ! command -v gh >/dev/null; then
    echo "Can't find 'gh' tool in PATH, please install from https://github.com/cli/cli"
    exit 1
fi

if [[ "$#" -ne 2 ]]; then
    echo "${0} <remote branch> <new-version>: bump version on <remote branch> and create a PR"
    echo ""
    echo "  Checks out <remote branch> and handles the version bump for you."
    echo "  Examples:"
    echo "    $0 upstream/master v1.13.0        # Bump to v1.13.0 based on upstream/master"
    echo "    $0 upstream/release-1.12 v1.12.1  # Bump to v1.12.1 based on upstream/release-1.12"
    echo ""
    echo "  Set the DRY_RUN environment var to skip git push and creating PR."
    echo "  When DRY_RUN is set the script will leave you in a branch containing the version changes."
    echo ""
    echo "  Set UPSTREAM_REMOTE (default: upstream) and FORK_REMOTE (default: origin)"
    echo "  To override the default remote names to what you have locally."
    exit 1
fi

declare -r BRANCH="$1"
declare -r NEW_VERSION="$2"

# Validate version format
if [[ ! "${NEW_VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+.*$ ]]; then
    echo "Error: Version must follow format vX.Y.Z (e.g., v1.12.0, v1.12.1-beta1)"
    exit 1
fi

# Checks if you are logged in. Will error/bail if you are not.
gh auth status

if git_status=$(git status --porcelain --untracked=no 2>/dev/null) && [[ -n "${git_status}" ]]; then
  echo "!!! Dirty tree. Clean up and try again."
  exit 1
fi

if [[ -e "${REBASEMAGIC}" ]]; then
  echo "!!! 'git rebase' or 'git am' in progress. Clean up and try again."
  exit 1
fi

# Extract chart version
CHART_VERSION=${NEW_VERSION#v}

echo "+++ Updating remotes..."
git remote update "${UPSTREAM_REMOTE}" "${FORK_REMOTE}"

if ! git log -n1 --format=%H "${BRANCH}" >/dev/null 2>&1; then
  echo "!!! '${BRANCH}' not found. The first argument should be something like ${UPSTREAM_REMOTE}/master or ${UPSTREAM_REMOTE}/release-1.12."
  echo "    (In particular, it needs to be a valid, existing remote branch that I can 'git checkout'.)"
  exit 1
fi

declare -r NEWBRANCHREQ="automated-version-bump-${NEW_VERSION}"
declare -r NEWBRANCH="$(echo "${NEWBRANCHREQ}-${BRANCH}" | sed 's/\//-/g')"
declare -r NEWBRANCHUNIQ="${NEWBRANCH}-$(date +%s)"

function return_to_kansas {
  # return to the starting branch and clean up
  local current_branch
  current_branch=$(git symbolic-ref --short HEAD 2>/dev/null || echo "")
  
  if [[ -n "${cleanbranch}" && "${current_branch}" == "${cleanbranch}" ]]; then
    # We're on the temporary branch we created
    if [[ -z "${SUCCESS_MARKER}" ]]; then
      echo ""
      echo "!!! Script failed during execution. Cleaning up uncommitted changes..."
      # Reset any uncommitted changes
      git reset --hard HEAD >/dev/null 2>&1 || true
      # Clean any untracked files that might have been created
      git clean -fd >/dev/null 2>&1 || true
      
      echo ""
      echo "+++ Returning you to the ${STARTINGBRANCH} branch and cleaning up."
      git checkout -f "${STARTINGBRANCH}" >/dev/null 2>&1 || true
      
      # Delete the temporary branch
      git branch -D "${cleanbranch}" >/dev/null 2>&1 || true
    elif [[ -n "${DRY_RUN}" ]]; then
      # DRY_RUN mode and successful - leave user in the branch to inspect changes
      echo ""
      echo "!!! DRY_RUN mode: Leaving you in branch ${cleanbranch} to inspect changes."
      echo "To return to the branch you were in when you invoked this script:"
      echo ""
      echo "  git checkout ${STARTINGBRANCH}"
      echo ""
      echo "To delete this branch:"
      echo ""
      echo "  git branch -D ${cleanbranch}"
    else
      # Non-DRY_RUN mode and successful - normal cleanup
      echo ""
      echo "+++ Returning you to the ${STARTINGBRANCH} branch and cleaning up."
      git checkout -f "${STARTINGBRANCH}" >/dev/null 2>&1 || true
      
      # Delete the temporary branch
      git branch -D "${cleanbranch}" >/dev/null 2>&1 || true
    fi
  fi
}
trap return_to_kansas EXIT

function make-a-pr() {
  local target_branch="$(basename "${BRANCH}")"
  echo
  echo "+++ Creating a pull request on GitHub at ${GITHUB_USER}:${NEWBRANCH}"

  local pr_body="This is an automated version bump created by \`hack/bump-version.sh\`.

**Target branch:** \`${BRANCH}\`
**New version:** \`${NEW_VERSION}\`

**Changes:**
- Update \`.release-version\` to \`${NEW_VERSION}\`
- Update Helm Chart version to \`${CHART_VERSION}\`
- Update default image tag to \`${NEW_VERSION}\`
- Update \`volcano.sh/apis\` dependency to \`${NEW_VERSION}\`
- Update development YAML files (\`volcano-development.yaml\`, \`volcano-agent-development.yaml\`, \`volcano-monitoring.yaml\`)
- Run \`go mod tidy\`

**Next steps after merging:**
1. Tag the release: \`git tag ${NEW_VERSION} && git push ${UPSTREAM_REMOTE} ${NEW_VERSION}\`
2. This will trigger automated image and chart publishing"

  gh pr create \
      --title "Automated: Bump version to ${NEW_VERSION}" \
      --body "${pr_body}" \
      --base "${target_branch}" \
      --head "${GITHUB_USER}:${NEWBRANCH}" \
      --repo "${MAIN_REPO_ORG}/${MAIN_REPO_NAME}"
}

echo "+++ Creating local branch ${NEWBRANCHUNIQ} based on ${BRANCH}"
git checkout -b "${NEWBRANCHUNIQ}" "${BRANCH}"
cleanbranch="${NEWBRANCHUNIQ}"

echo ""
echo "=== Bumping version to ${NEW_VERSION} ==="

# Update .release-version
echo "+++ Updating ${RELEASE_VERSION_FILE}"
echo "${NEW_VERSION}" > "${RELEASE_VERSION_FILE}"

# Update Chart.yaml
echo "+++ Updating ${CHART_YAML}"
if [[ -f "${CHART_YAML}" ]]; then
    sed -i.bak "s/^version: .*/version: \"${CHART_VERSION}\"/" "${CHART_YAML}"
    sed -i.bak "s/^appVersion: .*/appVersion: \"${CHART_VERSION}\"/" "${CHART_YAML}"
    rm -f "${CHART_YAML}.bak"
fi

# Update values.yaml
echo "+++ Updating ${VALUES_YAML}"
if [[ -f "${VALUES_YAML}" ]]; then
    sed -i.bak "s/image_tag_version: .*/image_tag_version: \"${NEW_VERSION}\"/" "${VALUES_YAML}"
    rm -f "${VALUES_YAML}.bak"
fi

# Update go.mod
echo "+++ Updating go.mod"
if [[ -f "go.mod" ]]; then
    sed -i.bak "s|volcano.sh/apis v.*|volcano.sh/apis ${NEW_VERSION}|" go.mod
    rm -f go.mod.bak
    
    echo "+++ Running go mod tidy"
    go mod tidy
fi

# Update development YAML files
echo "+++ Updating development YAML files"
if [[ -f "Makefile" ]]; then
    echo "+++ Running make TAG=${NEW_VERSION} update-development-yaml"
    make TAG="${NEW_VERSION}" update-development-yaml
fi

echo ""
echo "Files updated successfully!"

if [[ -n "${DRY_RUN}" ]]; then
  echo "!!! Skipping git push and PR creation because you set DRY_RUN."
  # Mark successful completion in DRY_RUN mode
  SUCCESS_MARKER="true"
  exit 0
fi

# Commit changes
echo "+++ Committing changes"
git add .
git commit -s -m "chore: bump version to ${NEW_VERSION}

- Update .release-version to ${NEW_VERSION}
- Update Chart version to ${CHART_VERSION}
- Update image tag version to ${NEW_VERSION}
- Update volcano.sh/apis dependency to ${NEW_VERSION}
- Update development YAML files"

if git remote -v | grep ^"${FORK_REMOTE}" | grep "${MAIN_REPO_ORG}/${MAIN_REPO_NAME}.git"; then
  echo "!!! You have ${FORK_REMOTE} configured as your ${MAIN_REPO_ORG}/${MAIN_REPO_NAME}.git"
  echo "This isn't normal. Leaving you with push instructions:"
  echo
  echo "+++ First manually push the branch this script created:"
  echo
  echo "  git push REMOTE ${NEWBRANCHUNIQ}:${NEWBRANCH}"
  echo
  echo "where REMOTE is your personal fork (maybe ${UPSTREAM_REMOTE}? Consider swapping those.)."
  echo "OR consider setting UPSTREAM_REMOTE and FORK_REMOTE to different values."
  echo
  make-a-pr
  cleanbranch=""
  exit 0
fi

# Push branch
git push "${FORK_REMOTE}" "${NEWBRANCHUNIQ}:${NEWBRANCH}"

make-a-pr

echo ""
echo "✅ Version bump completed successfully!"

# Mark successful completion
SUCCESS_MARKER="true" 