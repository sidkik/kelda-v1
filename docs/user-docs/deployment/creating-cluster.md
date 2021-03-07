# Creating the Kubernetes Cluster

Kelda requires a Kubernetes cluster in order to run your containers. Any
standard Kubernetes cluster will work.

This document covers a couple of approaches for getting this cluster.

---

## **Use an existing cluster (recommended)**

If your team already has a Kubernetes cluster for testing, you can deploy Kelda
to this cluster. Kelda won't affect any namespaces other than your development
namespace.

---

## **Ask your DevOps team to create a test cluster**

If your company already has a way of creating Kubernetes clusters (e.g. for
creating development clusters), just create a cluster that way.

The benefits are that you won't have to deal with creating the cluster
yourself, and you'll be sure that the cluster follows your company's security
policies.

All you'll need from your DevOps team after they create the cluster is a
[Kubeconfig](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)
file that can be used with `kubectl`.

---

## **Boot the cluster yourself (in the cloud)**

If you have cloud credentials for booting resources in the cloud, you can use
one of the hosted Kubernetes solutions to create a cluster in the cloud. We use
[Google Kubernetes Engine](https://cloud.google.com/kubernetes-engine/)
internally, but [Amazon](https://aws.amazon.com/eks/),
[Azure](https://azure.microsoft.com/en-us/services/kubernetes-service/) and
[DigitalOcean](https://www.digitalocean.com/products/kubernetes/) all have
equivalent services.

#### Booting on Amazon and Google

Our `qk8s` script makes it easy to spin up a cluster on Google and Amazon:

1. **Install dependencies**

    If you want to use **Amazon** you'll need to setup
[`kops`](https://github.com/kubernetes/kops/blob/master/docs/install.md).

    For **Google**, you'll need
    [`gcloud`](https://cloud.google.com/sdk/install). Make sure to run `gcloud
    auth login` so that `qk8s` will have the necessary credentials.

1. **Download qk8s**

    Run `curl -fsSL 'https://kelda.io/qk8s.sh' -o qk8s`.

1. **Make it executable**

    Run `chmod +x qk8s`.

1. **Run the script**

    For **Amazon**, run `./qk8s aws`.

    For **Google**, run `./qk8s gcp`.

---

## **Run the cluster locally**

There are a number of tools for booting a Kubernetes cluster on your local
machine. This can be a good option if you want to play around with Kelda.
However, you won't get the resource benefits of offloading the Kubernetes
cluster to the cloud.

#### [Docker for Mac](https://docs.docker.com/docker-for-mac/#kubernetes) (Recommended)

If you already have Docker installed, you should probably go with this approach.

Docker for Mac can create a Kubernetes cluster by running the Kubernetes
components in Docker containers.

You can enable it in the "Kubernetes" pane of the Docker For Mac preferences.
In the "Advanced" pane, make sure to allocate enough CPU and RAM to run your
application.

Once the cluster is ready, you can deploy Kelda to the `docker-desktop`
Kubeconfig context.

#### [Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube/)

Minikube is a VM-based version of Kubernetes for running locally. Rather than
running in containers, it boots an actual VM containing the Kubernetes
components.

To create the cluster, run `minikube start --cpus <num cpus> --memory <Mb of
RAM>`. Make sure to allocate enough CPU and RAM to run your application.

Once the cluster is ready, you can deploy Kelda to the `minikube` Kubeconfig
context.

#### Other Options

There are many other tools for creating local Kubernetes clusters, such as
[Kind](https://kind.sigs.k8s.io/), [Microk8s](https://microk8s.io/), and
[k3s](https://k3s.io/).

---

## **Use a Kelda-managed cluster (Beta)**

If you don't want the overhead of maintaining a Kubernetes cluster, we can run
it for you.

Request access to Kelda’s beta cloud service [here](/request-hosted-kelda-access)!
