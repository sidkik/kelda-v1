#!/bin/bash
set -euo pipefail

UPGRADE_TOKEN=ci KELDA_VERSION=0.0.0 make install
mv "$GOPATH/bin/kelda" "$GOPATH/bin/kelda-min-version"

UPGRADE_TOKEN=ci KELDA_VERSION=999.999.999 make install
mv "$GOPATH/bin/kelda" "$GOPATH/bin/kelda-max-version"

LATEST_RELEASE_URL="https://update.kelda.io/?file=kelda&release=latest&token=ci&os=linux"
# Download the latest release of Kelda from the S3 bucket.
curl -fLo "$GOPATH/bin/kelda-release.tar.gz" "$LATEST_RELEASE_URL"

# Extract the release
tar -C "$GOPATH/bin" -xzf "$GOPATH/bin/kelda-release.tar.gz"
