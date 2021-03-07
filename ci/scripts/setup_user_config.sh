#!/bin/bash
set -euo pipefail

magda_workspace_path="${CI_EXAMPLES_REPO_PATH}/magda/magda-kelda-config"
web_server_path="${CI_EXAMPLES_REPO_PATH}/magda/magda-web-server"

LATEST_RELEASE_URL="https://update.kelda.io/?file=kelda&release=latest&token=ci&os=linux"

if [[ "$CI_BACKWARDS_COMPAT" == "true" ]]; then
  # Backup the binary compiled from the master branch.
  cp "$GOPATH/bin/kelda" "$GOPATH/bin/kelda-master"

  # Download the latest release of Kelda from the S3 bucket.
  curl -fLo "$GOPATH/bin/kelda-release.tar.gz" "$LATEST_RELEASE_URL"

  # Extract the release, and keep a copy as "kelda-release".
  tar -C "$GOPATH/bin" -xzf "$GOPATH/bin/kelda-release.tar.gz"
  cp "$GOPATH/bin/kelda" "$GOPATH/bin/kelda-release"

  # Setup the minion with the latest release.
  kubectl delete namespace kelda || true
  kelda setup-minion --force --license "$(dirname "$0")/kelda-license"

  # Setup the dev namespace with the latest release.
  cd ${magda_workspace_path}
  yes 1 | USER=${CI_NAMESPACE} kelda config || true
  timeout --preserve-status -s INT 30 kelda dev --no-gui "${web_server_path}"
  rm ~/.kelda.yaml
  cd -

  # Setup the minion with the master branch (simulating updating the minion).
  cp "$GOPATH/bin/kelda-master" "$GOPATH/bin/kelda"
  kubectl delete namespace kelda || true
  kelda setup-minion --force --license "$(dirname "$0")/kelda-license"

  # Use the latest release as CLI for the rest of the test.
  cp "$GOPATH/bin/kelda-release" "$GOPATH/bin/kelda"

  cd "${magda_workspace_path}"
  yes 1 | USER=${CI_NAMESPACE} kelda config || true
else
  cd "${magda_workspace_path}"
  yes 1 | USER=${CI_NAMESPACE} kelda config || true
fi

# Setup NPM token. This is used when the gateway service is run in development
# mode.

cat << EOF > ~/.npmrc
cache-lock-stale=10
cache-lock-wait=10
//registry.npmjs.org/:_authToken=${NPM_TOKEN}
EOF
