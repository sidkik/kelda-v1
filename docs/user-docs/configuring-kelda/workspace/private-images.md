# Working with Private Images

The `regcred` secret in the `kelda` namespace is automatically copied to each
development namespace and added to the default Service Account.

Once you create the secret, pods booted by Kelda will be able to pull your
private images.

## Creating the Secret

??? warning "Every developer will have access to the registry login"

    We recommend using a service account instead of a user-specific Dockerhub
    account unless you're the only user on the cluster.

You can create the required secret by running the following command:

    kubectl create secret docker-registry regcred -n kelda \
        --docker-server <REGISTRY URL> \
        --docker-username=<USERNAME> \
        --docker-password=<PASSWORD> \
        --docker-email=<EMAIL>

If you're using DockerHub, use `https://index.docker.io/v1/` as the `<REGISTRY
URL>`. If you're using GCR, use `gcr.io`.

## Updating the Secret

If the secret already exists, you'll need to remove it before the above command
will work.

    kubectl delete secret -n kelda regcred
