#!/bin/bash
set -euo pipefail

source "$(dirname "$0")/gce_cluster_common.sh"

NODE_COUNT=2
NODE_SIZE=custom-4-6144

gsutil mb -p $PROJECT $KOPS_STATE_STORE
kops create cluster $CLUSTER_DOMAIN --cloud gce --project=$PROJECT \
    --zones $ZONE --master-size n1-standard-1 --node-count $NODE_COUNT --node-size $NODE_SIZE \
    --kubernetes-version $KUBERNETES_VERSION --vpc $CLUSTER_NAME
kops update cluster $CLUSTER_DOMAIN --yes

# Wait until all nodes are up. We expect there to be an additional node than we
# specified to Kops because the `kubectl get nodes` output includes the master
# node.
let exp_count=${NODE_COUNT}+1

set +eo pipefail
while true; do
    nodes=$(timeout 2 kubectl get nodes 2> /dev/null)
    ready_count=$(grep "\bReady\b" <<<$nodes | wc -l)
    if [[ $ready_count == $exp_count ]]; then
        break
    fi

    echo "Nodes not Ready. Will check again in 30s."
    echo "${nodes}"
    echo

    sleep 30
done
