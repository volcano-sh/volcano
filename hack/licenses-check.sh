#!/bin/bash

if [[ -n $(git status --porcelain) ]]; then
  git status
  git diff
  echo "ERROR: Some files need to be updated, please run 'make mirror-licenses' and include any changed files in your PR"
  exit 1
fi