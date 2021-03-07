PROJECT=kelda-ci
ZONE=us-central1-a
export KOPS_FEATURE_FLAGS=AlphaAllowGCE

# Allow running older versions of Kubernetes, such as 1.9.0.
export KOPS_RUN_OBSOLETE_VERSION=true

DEPENDENCY_SATISFIED=true

function check_available() {
    if ! $@ > /dev/null 2>&1 ; then
        echo "$1 is required to run this script."
        DEPENDENCY_SATISFIED=false
    fi
}

if [[ ${1+set} != set ]]; then
    echo "usage:   $0 kubernetes_version"
    echo "example: $0 1.9.0"
    exit 1
fi

KUBERNETES_VERSION=$1
CLUSTER_NAME=kelda-ci-${KUBERNETES_VERSION//./}
CLUSTER_DOMAIN=$CLUSTER_NAME.k8s.local
export KOPS_STATE_STORE=gs://$CLUSTER_NAME/

check_available kops version
check_available kubectl version --client=true
check_available gcloud version

if [[ $DEPENDENCY_SATISFIED != "true" ]]; then
    exit 1
fi

# Setup gcloud credentials.
base64 -d <<<"$GCLOUD_SERVICE_KEY" > gcloud_service_key.json
gcloud auth activate-service-account --key-file=gcloud_service_key.json
export GOOGLE_APPLICATION_CREDENTIALS="$(pwd)/gcloud_service_key.json"
