#!/usr/bin/env bash

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

# This script manages the staging/src/volcano.sh/apis directory.
# It can initialize the staging directory from the apis repo, or sync changes to it.
#
# Usage:
#   ./hack/sync-apis.sh [command] [options]
#
# Commands:
#   init               Initialize staging directory from volcano-sh/apis repo
#   sync (default)     Sync staging directory to volcano-sh/apis repo
#
# Options:
#   --push             Push changes to remote (default: false)
#   --create-pr        Create a pull request after pushing (requires --push, default: false)
#   --branch NAME      Use specified branch name (default: sync-YYYYMMDD-HHMMSS)
#   --target-branch    Target branch in apis repo (default: master)
#   --force            Force overwrite existing staging directory (for init)
#   --help             Show this help message
#
# Environment variables:
#   APIS_REPO_URL      URL of the apis repository to push to (default: https://github.com/volcano-sh/apis.git)
#                      For your fork, use: https://github.com/YOUR_USERNAME/apis.git
#   APIS_REPO          Repository name to push to in format owner/repo (default: volcano-sh/apis)
#                      For your fork, use: YOUR_USERNAME/apis
#   TARGET_APIS_REPO   Target repository for PR creation (default: volcano-sh/apis)
#                      When APIS_REPO is your fork, this should be the upstream repo
#   Note: GitHub CLI (gh) is required for --create-pr. Install from https://cli.github.com/

set -o errexit
set -o nounset
set -o pipefail

# Get the root of the repo
SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${SCRIPT_ROOT}"

# Configuration
STAGING_DIR="${SCRIPT_ROOT}/staging/src/volcano.sh/apis"
APIS_REPO_URL=${APIS_REPO_URL:-"https://github.com/volcano-sh/apis.git"}
# Use mktemp for secure temporary directory creation if not provided via environment
# This prevents race conditions and security issues with fixed paths
APIS_REPO_DIR=${APIS_REPO_DIR:-""}
TARGET_BRANCH=${TARGET_BRANCH:-"master"}
BRANCH_NAME=""
PUSH=false
CREATE_PR=false
FORCE=false
COMMAND="sync"
# Track if we created the temp directory ourselves (for cleanup)
APIS_REPO_DIR_CREATED=false
# APIS repo name (e.g., volcano-sh/apis or PersistentJZH/apis for fork)
# This is the repo to push to (your fork)
APIS_REPO=${APIS_REPO:-"volcano-sh/apis"}
# Target APIS repo for PR creation (the original repo to create PR against)
# Defaults to volcano-sh/apis, but can be set to create PR from fork to upstream
TARGET_APIS_REPO=${TARGET_APIS_REPO:-"volcano-sh/apis"}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Cleanup function for error handling
cleanup() {
    local exit_code=$?
    # Clean up temporary directory if we created it
    if [[ "${APIS_REPO_DIR_CREATED}" == "true" ]] && [[ -n "${APIS_REPO_DIR}" ]] && [[ -d "${APIS_REPO_DIR}" ]]; then
        print_info "Cleaning up temporary directory: ${APIS_REPO_DIR}"
        rm -rf "${APIS_REPO_DIR}"
    fi
    if [[ ${exit_code} -ne 0 ]]; then
        print_error "Script failed with exit code ${exit_code}"
    fi
}
trap cleanup EXIT

show_help() {
    cat << EOF
Usage: $0 [command] [options]

Manage the staging/src/volcano.sh/apis directory.

Commands:
    init                Initialize staging directory from volcano-sh/apis repo
    sync (default)      Sync staging directory to volcano-sh/apis repo

Options:
    --push              Push changes to remote (sync only, default: false)
    --create-pr         Create a pull request after pushing (requires --push, default: false)
    --branch NAME       Use specified branch name (sync only, default: sync-YYYYMMDD-HHMMSS)
    --target-branch     Target branch in apis repo (default: master)
    --force             Force overwrite existing staging directory (init only)
    --help              Show this help message

Environment Variables:
    APIS_REPO_URL       URL of the apis repository (default: https://github.com/volcano-sh/apis.git)
    APIS_REPO_DIR       Local directory for cloning (default: auto-created via mktemp)
    TARGET_BRANCH       Target branch in apis repo (default: master)

Examples:
    # Initialize staging directory for the first time
    $0 init

    # Re-initialize staging directory (overwrite existing)
    $0 init --force

    # Initialize from a specific branch
    $0 init --target-branch release-1.9

    # Sync changes locally
    $0 sync

    # Sync and push to remote
    $0 sync --push

    # Sync, push, and create PR
    $0 sync --push --create-pr

    # Sync to a specific branch
    $0 sync --branch my-sync-branch --push
EOF
}

# Initialize staging directory from apis repo
do_init() {
    print_info "Initializing staging directory from ${APIS_REPO_URL}..."
    
    # Check if staging directory already has content
    if [[ -d "${STAGING_DIR}" ]] && [[ -n "$(ls -A "${STAGING_DIR}" 2>/dev/null)" ]]; then
        if [[ "${FORCE}" != "true" ]]; then
            print_error "Staging directory already exists and is not empty: ${STAGING_DIR}"
            print_info "Use --force to overwrite existing content"
            exit 1
        fi
        print_warning "Force mode: will overwrite existing staging directory"
    fi
    
    # Create staging directory if it doesn't exist
    mkdir -p "${STAGING_DIR}"
    
    # Clone apis repo to temp directory
    # Use mktemp for secure temporary directory creation
    local TEMP_DIR=$(mktemp -d /tmp/volcano-apis-init.XXXXXX)
    print_info "Cloning apis repository (branch: ${TARGET_BRANCH})..."
    
    if ! git clone --depth 1 --branch "${TARGET_BRANCH}" "${APIS_REPO_URL}" "${TEMP_DIR}" 2>&1; then
        print_error "Failed to clone. Does branch '${TARGET_BRANCH}' exist in ${APIS_REPO_URL}?"
        exit 1
    fi
    
    # Copy contents to staging directory (excluding .git and .github)
    print_info "Copying to staging directory..."
    rsync -av --delete \
        --exclude='.git' \
        --exclude='.github' \
        "${TEMP_DIR}/" "${STAGING_DIR}/"
    
    # Clean up temp directory
    rm -rf "${TEMP_DIR}"
    
    print_success "Staging directory initialized successfully!"
    print_info "Location: ${STAGING_DIR}"
    print_info ""
    print_info "Next steps:"
    print_info "  1. The go.mod should have a replace directive:"
    print_info "     replace volcano.sh/apis => ./staging/src/volcano.sh/apis"
    print_info "  2. Make API changes in ${STAGING_DIR}"
    print_info "  3. Run 'go build ./...' to verify changes"
    print_info "  4. Commit and create a PR"
}

# Sync staging directory to apis repo
do_sync() {
    # Verify staging directory exists and has content
    if [[ ! -d "${STAGING_DIR}" ]]; then
        print_error "Staging directory does not exist: ${STAGING_DIR}"
        print_info "Run '$0 init' to initialize it first"
        exit 1
    fi
    
    if [[ -z "$(ls -A "${STAGING_DIR}" 2>/dev/null)" ]]; then
        print_error "Staging directory is empty: ${STAGING_DIR}"
        print_info "Run '$0 init' to initialize it first"
        exit 1
    fi
    
    # Set default branch name if not specified
    if [[ -z "${BRANCH_NAME}" ]]; then
        BRANCH_NAME="sync-$(date +%Y%m%d-%H%M%S)"
    fi
    
    # Get current commit info from main repo
    VOLCANO_SHA=$(git rev-parse HEAD)
    VOLCANO_SHA_SHORT=$(git rev-parse --short HEAD)
    VOLCANO_COMMIT_MSG=$(git log -1 --pretty=format:"%s")
    VOLCANO_AUTHOR=$(git log -1 --pretty=format:"%an")
    VOLCANO_EMAIL=$(git log -1 --pretty=format:"%ae")
    
    print_info "Source commit: ${VOLCANO_SHA_SHORT} - ${VOLCANO_COMMIT_MSG}"
    print_info "Author: ${VOLCANO_AUTHOR} <${VOLCANO_EMAIL}>"
    
    # Create temporary directory if not provided via environment variable
    if [[ -z "${APIS_REPO_DIR}" ]]; then
        APIS_REPO_DIR=$(mktemp -d /tmp/volcano-apis-sync.XXXXXX)
        APIS_REPO_DIR_CREATED=true
        print_info "Created temporary directory: ${APIS_REPO_DIR}"
    else
        # If directory was provided, ensure it doesn't exist or remove it
        if [[ -d "${APIS_REPO_DIR}" ]]; then
            print_info "Removing existing directory: ${APIS_REPO_DIR}"
            rm -rf "${APIS_REPO_DIR}"
        fi
    fi
    
    print_info "Cloning apis repository from ${APIS_REPO_URL}..."
    if ! git clone --depth 1 --branch "${TARGET_BRANCH}" "${APIS_REPO_URL}" "${APIS_REPO_DIR}" 2>&1; then
        print_error "Failed to clone. Does branch '${TARGET_BRANCH}' exist in ${APIS_REPO_URL}?"
        exit 1
    fi
    
    # Sync files
    # Exclude .github because it contains repo-specific configs (issue templates, workflows)
    # that should remain in the apis repo
    print_info "Syncing staging directory to apis repo..."
    rsync -av --delete \
        --exclude='.git' \
        --exclude='.github' \
        "${STAGING_DIR}/" "${APIS_REPO_DIR}/"
    
    # Check for changes
    cd "${APIS_REPO_DIR}"
    
    if git diff --quiet && git diff --staged --quiet; then
        print_success "No changes to sync. APIs are up to date."
        echo "SYNC_RESULT=no_changes"
        return 0
    fi
    
    print_info "Changes detected:"
    git status --short
    
    # Create branch and commit
    print_info "Creating branch: ${BRANCH_NAME}"
    git checkout -b "${BRANCH_NAME}"
    
    print_info "Committing changes..."
    git config user.name "${VOLCANO_AUTHOR}"
    git config user.email "${VOLCANO_EMAIL}"
    
    git add .
    git commit -s -m  "Sync from volcano-sh/volcano@${VOLCANO_SHA_SHORT}

Original commit: ${VOLCANO_COMMIT_MSG}

Automated sync from staging directory.
Source: https://github.com/volcano-sh/volcano/commit/${VOLCANO_SHA}"
    
    if [[ "${PUSH}" == "true" ]]; then
        print_info "Pushing to remote..."
        if ! git push origin "${BRANCH_NAME}" 2>&1; then
            print_error "Failed to push to ${APIS_REPO}"
            print_info "Common causes:"
            print_info "  - No write access to ${APIS_REPO}"
            print_info "  - Branch '${BRANCH_NAME}' already exists remotely"
            print_info "  - Network connectivity issues"
            print_info "  - Invalid credentials"
            exit 1
        fi
        print_success "Changes pushed to origin/${BRANCH_NAME}"
        
        # Create PR if requested
        if [[ "${CREATE_PR}" == "true" ]]; then
            create_pull_request
        else
            print_info "Create a PR at: https://github.com/${APIS_REPO}/compare/${TARGET_BRANCH}...${BRANCH_NAME}"
        fi
        # If we created the temp directory and push succeeded, we can mark it for cleanup
        # (cleanup will happen in trap handler)
    else
        print_success "Changes committed locally."
        print_info "To push changes, run:"
        echo "    cd ${APIS_REPO_DIR} && git push origin ${BRANCH_NAME}"
        # If not pushing, keep the directory for user to manually push
        # Only cleanup if we created it and user doesn't need it
        if [[ "${APIS_REPO_DIR_CREATED}" == "true" ]]; then
            print_info "Temporary directory preserved at: ${APIS_REPO_DIR}"
            print_info "It will be cleaned up on script exit."
        fi
    fi
    
    echo "SYNC_RESULT=success"
    print_success "Sync complete!"
}

# Check if PR already exists
check_existing_pr() {
    local repo=$1
    local head=$2
    
    if ! command -v gh >/dev/null 2>&1; then
        return 1
    fi
    
    local existing=$(gh pr list --repo "${repo}" --head "${head}" --json number --jq '.[0].number' 2>/dev/null || echo "")
    if [[ -n "${existing}" ]]; then
        echo "${existing}"
        return 0
    fi
    return 1
}

# Generate PR title
generate_pr_title() {
    local source_repo=$1
    local sha_short=$2
    echo "Sync APIs from ${source_repo}@${sha_short}"
}

# Generate PR body
generate_pr_body() {
    local source_repo=$1
    local source_branch=$2
    local sha_short=$3
    local sha=$4
    local author=$5
    
    # Check if running in GitHub Actions
    local GITHUB_ACTOR=${GITHUB_ACTOR:-}
    local GITHUB_EVENT_NAME=${GITHUB_EVENT_NAME:-}
    local GITHUB_RUN_NUMBER=${GITHUB_RUN_NUMBER:-}
    local GITHUB_RUN_ID=${GITHUB_RUN_ID:-}
    local GITHUB_SERVER_URL=${GITHUB_SERVER_URL:-https://github.com}
    local GITHUB_REPOSITORY=${GITHUB_REPOSITORY:-${source_repo}}
    local GITHUB_REF_NAME=${GITHUB_REF_NAME:-${source_branch}}
    
    if [[ -n "${GITHUB_ACTOR}" ]]; then
        # Running in GitHub Actions
        cat <<EOF
## Automated API Sync

This PR was automatically created to sync API changes from the main volcano repository.

### Source Information
| Item | Value |
|------|-------|
| Repository | ${source_repo} |
| Branch | \`${source_branch}\` |
| Commit | [\`${sha_short}\`](https://github.com/${source_repo}/commit/${sha}) |

### Triggered by
| Item | Value |
|------|-------|
| Actor | @${GITHUB_ACTOR} |
| Event | \`${GITHUB_EVENT_NAME}\` |
| Run | [#${GITHUB_RUN_NUMBER}](${GITHUB_SERVER_URL}/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}) |

---
> 🤖 This PR was generated by [sync-apis workflow](${GITHUB_SERVER_URL}/${GITHUB_REPOSITORY}/blob/${GITHUB_REF_NAME}/.github/workflows/sync-apis.yaml)
EOF
    else
        # Running locally
        cat <<EOF
## Automated API Sync

This PR was automatically created to sync API changes from the main volcano repository.

### Source Information
| Item | Value |
|------|-------|
| Repository | ${source_repo} |
| Branch | \`${source_branch}\` |
| Commit | [\`${sha_short}\`](https://github.com/${source_repo}/commit/${sha}) |
| Author | ${author} |

### Changes
This PR syncs API definitions from the staging directory in the main repository.

---
> 🤖 This PR was generated by \`hack/sync-apis.sh\`
EOF
    fi
}

# Create pull request in apis repo
create_pull_request() {
    print_info "Creating pull request..."
    
    # Determine if this is a cross-repo PR (from fork to upstream)
    local IS_CROSS_REPO=false
    local FORK_OWNER=""
    local PR_HEAD="${BRANCH_NAME}"
    
    if [[ "${APIS_REPO}" != "${TARGET_APIS_REPO}" ]]; then
        IS_CROSS_REPO=true
        FORK_OWNER=$(echo "${APIS_REPO}" | cut -d'/' -f1)
        PR_HEAD="${FORK_OWNER}:${BRANCH_NAME}"
        print_info "Cross-repo PR: from ${APIS_REPO} to ${TARGET_APIS_REPO}"
    fi
    
    # Get source repository info
    local SOURCE_REPO=""
    local SOURCE_BRANCH=""
    local SOURCE_REPO_URL=$(git -C "${SCRIPT_ROOT}" remote get-url origin 2>/dev/null || echo "")
    
    if [[ -n "${SOURCE_REPO_URL}" ]]; then
        # Extract repo name from URL (handle both https and ssh formats)
        if [[ "${SOURCE_REPO_URL}" =~ github.com[:/]([^/]+/[^/]+?)(\.git)?$ ]]; then
            SOURCE_REPO="${BASH_REMATCH[1]}"
            # Remove .git suffix if present
            SOURCE_REPO="${SOURCE_REPO%.git}"
        fi
    fi
    
    # Default to volcano-sh/volcano if we can't detect it
    SOURCE_REPO=${SOURCE_REPO:-"volcano-sh/volcano"}
    SOURCE_BRANCH=$(git -C "${SCRIPT_ROOT}" branch --show-current 2>/dev/null || echo "unknown")
    
    # Generate PR title and body separately to avoid parsing issues
    local PR_TITLE=$(generate_pr_title "${SOURCE_REPO}" "${VOLCANO_SHA_SHORT}")
    local PR_BODY=$(generate_pr_body \
        "${SOURCE_REPO}" \
        "${SOURCE_BRANCH}" \
        "${VOLCANO_SHA_SHORT}" \
        "${VOLCANO_SHA}" \
        "${VOLCANO_AUTHOR}")
    
    # Determine PR head (branch name)
    local PR_HEAD="${BRANCH_NAME}"
    if [[ "${IS_CROSS_REPO}" == "true" ]]; then
        # Cross-repo PR: head format is "fork_owner:branch_name"
        PR_HEAD="${FORK_OWNER}:${BRANCH_NAME}"
    fi
    
    # Check if GitHub CLI is available
    if ! command -v gh >/dev/null 2>&1; then
        print_error "GitHub CLI (gh) is required to create PRs"
        print_info "Please install it: https://cli.github.com/"
        print_info "Or create PR manually: https://github.com/${TARGET_APIS_REPO}/compare/${TARGET_BRANCH}...${PR_HEAD}"
        return 1
    fi
    
    # Check if PR already exists
    local EXISTING_PR=$(check_existing_pr "${TARGET_APIS_REPO}" "${PR_HEAD}" || echo "")
    
    if [[ -n "${EXISTING_PR}" ]]; then
        print_warning "PR #${EXISTING_PR} already exists for branch ${PR_HEAD}"
        print_info "View PR at: https://github.com/${TARGET_APIS_REPO}/pull/${EXISTING_PR}"
        return 0
    fi
    
    # Create PR using GitHub CLI
    print_info "Creating pull request using GitHub CLI..."
    
    if gh pr create \
        --repo "${TARGET_APIS_REPO}" \
        --base "${TARGET_BRANCH}" \
        --head "${PR_HEAD}" \
        --title "${PR_TITLE}" \
        --body "${PR_BODY}" 2>&1; then
        print_success "Pull request created successfully!"
        print_info "View PR at: https://github.com/${TARGET_APIS_REPO}/pulls"
        return 0
    else
        print_error "Failed to create PR using GitHub CLI"
        print_info "Please check:"
        print_info "  1. GitHub CLI is authenticated: gh auth status"
        print_info "  2. You have permission to create PRs in ${TARGET_APIS_REPO}"
        if [[ "${IS_CROSS_REPO}" == "true" ]]; then
            print_info "  3. Manual PR link: https://github.com/${TARGET_APIS_REPO}/compare/${TARGET_BRANCH}...${PR_HEAD}"
        else
            print_info "  3. Manual PR link: https://github.com/${TARGET_APIS_REPO}/compare/${TARGET_BRANCH}...${BRANCH_NAME}"
        fi
        return 1
    fi
}

# Parse command (first non-option argument)
if [[ $# -gt 0 ]] && [[ "$1" != --* ]]; then
    COMMAND="$1"
    shift
fi

# Parse options
while [[ $# -gt 0 ]]; do
    case $1 in
        --push)
            PUSH=true
            shift
            ;;
        --create-pr)
            CREATE_PR=true
            shift
            ;;
        --force)
            FORCE=true
            shift
            ;;
        --branch)
            BRANCH_NAME="$2"
            shift 2
            ;;
        --target-branch)
            TARGET_BRANCH="$2"
            shift 2
            ;;
        --help)
            show_help
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Validate options
if [[ "${CREATE_PR}" == "true" ]] && [[ "${PUSH}" != "true" ]]; then
    print_error "--create-pr requires --push"
    print_info "Use: $0 sync --push --create-pr"
    exit 1
fi

# Execute command
case "${COMMAND}" in
    init)
        do_init
        ;;
    sync)
        do_sync
        ;;
    *)
        print_error "Unknown command: ${COMMAND}"
        show_help
        exit 1
        ;;
esac
