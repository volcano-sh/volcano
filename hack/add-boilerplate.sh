#!/bin/bash

# Add Volcano copyright header to .go files which missed it, and set year to earliest commit year

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")
REPO_ROOT=$(cd "${SCRIPT_ROOT}/.." && pwd)
BOILERPLATE_FILE="${SCRIPT_ROOT}/boilerplate/boilerplate.go.txt"

if [[ ! -f "${BOILERPLATE_FILE}" ]]; then
    echo "Error: Cannot find boilerplate template file ${BOILERPLATE_FILE}"
    exit 1
fi

get_earliest_year() {
    local file="$1"
    local year=$(git log --follow --format=%ad --date=format:%Y -- "$file" | tail -1)
    if [[ -z "$year" ]]; then
        year=$(date +%Y)
    fi
    echo "$year"
}

has_copyright() {
    local file="$1"
    # Check first 15 lines for any copyright header
    head -15 "$file" | grep -q "Copyright.*Authors"
}

add_copyright() {
    local file="$1"
    local year="$2"
    
    echo "Adding copyright header to $file (year: $year)"
    
    local temp_file=$(mktemp)
    sed "s/YEAR/$year/g" "${BOILERPLATE_FILE}" > "$temp_file"
    echo "" >> "$temp_file"
    cat "$file" >> "$temp_file"
    mv "$temp_file" "$file"
}

process_files() {
    local count=0
    
    while IFS= read -r -d '' file; do
        if ! has_copyright "$file"; then
            local year=$(get_earliest_year "$file")
            add_copyright "$file" "$year"
            ((count++))
        fi
    done < <(find "${REPO_ROOT}" -name "*.go" -not -path "*/vendor/*" -print0)
    
    echo "Processing completed. Added copyright headers to $count files."
}

main() {
    echo "Starting to check and add copyright headers..."
    
    if ! git rev-parse --git-dir >/dev/null 2>&1; then
        echo "Error: This script must be run within a git repository"
        exit 1
    fi
    
    process_files
}

main "$@" 