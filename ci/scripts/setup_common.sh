#!/bin/bash
set -euo pipefail

if [[ ! -f ~/.kube/config ]]; then
  "$(dirname "$0")/delete_gce_cluster.sh" "$KUBERNETES_VERSION" || true
  "$(dirname "$0")/create_gce_cluster.sh" "$KUBERNETES_VERSION"
fi

# Clone the repositories that will be tested.
git clone --single-branch --branch next-version git@github.com:kelda-inc/examples /tmp/examples
