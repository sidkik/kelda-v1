#!/bin/bash
set -eo pipefail
if [[ $1 == "" || $2 == "" ]]; then
    echo "usage: ./download-artifacts.sh START_NUM END_NUM"
    exit 1
fi
if [[ $CIRCLE_TOKEN == "" ]]; then
    echo "Please set the CIRCLE_TOKEN environment variable."
    exit 1
fi

for ((build_number=$1; build_number<=$2; build_number++)); do
    echo "Retrieving list of artifacts for build $build_number"
    files=`curl -s https://circleci.com/api/v1.1/project/github/kelda-inc/kelda/$build_number/artifacts?circle-token=$CIRCLE_TOKEN`
    if [[ $files != "[ ]" ]]; then
        echo "Artifacts found for build $build_number. Downloading..."
        mkdir -p "artifacts/$build_number"

        # Fetch only .tar.gz files as CircleCI will list ALL files.
        artifactURLs=$(curl -fsS https://circleci.com/api/v1.1/project/github/kelda-inc/kelda/$build_number/artifacts?circle-token=$CIRCLE_TOKEN \
        | grep -o 'https://[^"]*.tar.gz')
        while read -r url; do
            filename=$(basename $url)
            echo "$url"
            curl -fS#o "artifacts/$build_number/${filename}" "$url?circle-token=$CIRCLE_TOKEN"
            echo
        done <<< "$artifactURLs"
    fi
done

# Extract all files
echo "Extracting all files"
for file in `find "artifacts" -type f -iname "*.tar.gz"`; do
    dest=${file%.tar.gz}
    mkdir -p "$dest"
    tar -C "$dest" -xzf "$file"
done
