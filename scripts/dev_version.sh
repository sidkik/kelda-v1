#!/bin/bash
set -e

# Calculate the Git SHA of the repo (including uncommitted changes) by creating
# a temporary Git index, adding to it, and getting its hash.
# tmp_index=$(mktemp)

# cp .git/index ${tmp_index}
# GIT_INDEX_FILE="${tmp_index}" git add .
# hash=$(GIT_INDEX_FILE="${tmp_index}" git write-tree)
# rm ${tmp_index}

echo "$(git describe --tags --abbrev=0)"
