#!/bin/bash
set -euo pipefail

# This script generates a Kubeconfig that can be used to connect to the current
# Kubernetes context. It generates a service account with cluster-admin access.

# Create the user.
kubectl create serviceaccount kelda-user >/dev/null
kubectl create clusterrolebinding kelda-user --clusterrole=cluster-admin --serviceaccount=default:kelda-user >/dev/null

# Read the current settings.
context="$(kubectl config current-context)"
cluster="$(kubectl config view -o "jsonpath={.contexts[?(@.name==\"$context\")].context.cluster}")"
server="$(kubectl config view -o "jsonpath={.clusters[?(@.name==\"$cluster\")].cluster.server}")"
secret="$(kubectl get serviceaccount kelda-user -o 'jsonpath={.secrets[0].name}' 2>/dev/null)"
ca_crt_data="$(kubectl get secret "$secret" -o "jsonpath={.data.ca\.crt}" | openssl enc -d -base64 -A)"
token="$(kubectl get secret "$secret" -o "jsonpath={.data.token}" | openssl enc -d -base64 -A)"

# Write them to a file.
export KUBECONFIG="$(mktemp)"
kubectl config set-credentials kelda-user --token="$token" >/dev/null
ca_crt="$(mktemp)"; echo "$ca_crt_data" > $ca_crt
kubectl config set-cluster kelda-cluster --server="$server" --certificate-authority="$ca_crt" --embed-certs >/dev/null
kubectl config set-context kelda --cluster=kelda-cluster --user=kelda-user >/dev/null
kubectl config use-context kelda >/dev/null

cat "$KUBECONFIG"
