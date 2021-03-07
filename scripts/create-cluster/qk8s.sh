#!/bin/bash
set -euo pipefail

RED="$(tput setaf 1)"
GREEN="$(tput setaf 2)"
YELLOW="$(tput setaf 3)"
BLUE="$(tput setaf 4)"
END="$(tput sgr0)"

function ecol () {
    printf "$1$2$END\n"
}

function iprompt () {
    while true; do
	printf "$1 [y/N] "
	read -r yn
	case $yn in
	    [Yy] ) return 0;;
	    [Nn]|"") return 1;;
	esac
    done
}

GCP_ZONE='us-central1-a'
GCP_INSTANCE_TYPE='n1-standard-2'
GCP_INSTANCE_N=2

AWS_ZONE='us-west-2a'
AWS_INSTANCE_TYPE="t2.medium"
AWS_INSTANCE_N=2

if [ "$#" -eq 0 ]; then
    echo "Usage: qk8s provider [ cluster-name ], where provider can be 'aws' or 'gcp'"
    exit 1
fi

PROVIDER="$1"
CLUSTER_NAME="${2:-kelda-demo}"

function assert_kubectl () {
    if [ ! $(command -v kubectl) ]; then
       ecol $RED "Please ensure kubectl is installed."
       echo "On MacOS, please follow these instructions:"
       echo "https://kubernetes.io/docs/tasks/tools/install-kubectl/#install-kubectl-on-macos"
    fi
}

function assert_gcloud () {
    if [ ! $(command -v gcloud) ]; then
	ecol $RED "Please ensure the gcloud CLI is installed."
	echo "On MacOS, please follow these instructions:"
	echo "https://cloud.google.com/sdk/docs/quickstart-macos"
    fi
}

function assert_kops () {
    if [ ! $(command -v kops) ]; then
	ecol $RED "Please ensure the kops command is installed."
	echo "To do so, please follow these instructions:"
	echo "https://github.com/kubernetes/kops#installing"
    fi
}

# Actually uses GKE, not GCP VMs
function create_gcp_cluster () {
    assert_gcloud

    ecol $YELLOW "creating Kubernetes cluster on GKE..."

    gcloud container clusters create "$CLUSTER_NAME" \
          --zone="$GCP_ZONE" \
          --machine-type="$GCP_INSTANCE_TYPE" \
          --num-nodes="$GCP_INSTANCE_N" \
          --verbosity error

    # We have to install the credentials ourselves, kops will
    # do it itself
    gcloud container clusters get-credentials "$CLUSTER_NAME" \
	   --zone "$GCP_ZONE"

    echo
    ecol $GREEN "Cluster booted successfully."
    echo "Visit https://console.cloud.google.com/kubernetes/clusters/details/$GCP_ZONE/$CLUSTER_NAME for more information."
    echo "Once you are done, you can delete the cluster with the following command:"
    ecol $BLUE "    gcloud container clusters delete $CLUSTER_NAME --zone $GCP_ZONE"

    if iprompt "If you have completed the quickstart, would you like to deprovision the cluster?"; then
	    gcloud container clusters delete "$CLUSTER_NAME" --zone "$GCP_ZONE"
	    ecol $YELLOW "Cluster deprovisioned."
    else
	    ecol $YELLOW "Please deprovision the cluster yourself once you are finished using the commands printed above."
	    exit 0
    fi

    exit 0
}

function create_aws_cluster () {
    assert_kops

    ecol $YELLOW "Creating Kubernetes cluster on AWS..."

    # Create the S3 bucket for kops state storage
    echo "Please enter a name for provisioning the kops state storage S3 bucket:"
    read kops_state_storage
    STATE_BUCKET="kelda-demo-$kops_state_storage"

    # Use us-east-1 because other regions require special policies
    # https://github.com/kubernetes/kops/blob/master/docs/aws.md#cluster-state-storage
    aws s3api create-bucket --bucket "$STATE_BUCKET" --region us-east-1

    # .k8s.local signals kops to provision a gossip DNS-based cluster
    # so we don't have to set up or use Route53
    kops create cluster \
	 --name "$CLUSTER_NAME.k8s.local" \
	 --state "s3://$STATE_BUCKET" \
	 --zones us-west-2a \
	 --node-size "$AWS_INSTANCE_TYPE" \
	 --node-count "$AWS_INSTANCE_N" \
	 --yes

    echo
    ecol $YELLOW "Please wait a few minutes while the cluster finishes booting."
    echo "Check the output of the following command until the cluster reports ready:"
    ecol $BLUE "    kops validate cluster --state=s3://$STATE_BUCKET"
    echo
    echo "Once you are done, run the following two commands to deprovision the cluster:"
    ecol $BLUE "    kops delete cluster --state=s3://$STATE_BUCKET --name=$CLUSTER_NAME.k8s.local --yes"
    ecol $BLUE "    aws s3api delete-bucket --bucket $STATE_BUCKET"

    if iprompt "If you have completed the quickstart, would you like to unprovision the cluster now?"; then
	    kops delete cluster --state="s3://$STATE_BUCKET" --name="$CLUSTER_NAME.k8s.local" --yes
	    aws s3api delete-bucket --bucket "$STATE_BUCKET"
	    ecol $YELLOW "Cluster deprovisioned."
    else
	    ecol $YELLOW "Please deprovision the cluster yourself once you are finished."
	    exit 0
    fi

    exit 0
}

function main () {
    assert_kubectl

    if [ "$PROVIDER" = "aws" ]; then
	create_aws_cluster
    elif [ "$PROVIDER" = "gcp" ]; then
	create_gcp_cluster
    fi
}

main
