#!/bin/bash
set -euo pipefail

source "$(dirname "$0")/gce_cluster_common.sh"

# Don't crash if the cluster doesn't exist.
set +e

kops delete cluster $CLUSTER_DOMAIN --yes
gsutil rb $KOPS_STATE_STORE

# Delete dangling disks.
disks=$(gcloud compute disks list --project="$PROJECT" --filter="$CLUSTER_NAME" --format="value(name)")
for disk in $disks; do
    echo y | gcloud compute disks delete "$disk" --project="$PROJECT" --zone="$ZONE"
done
