# The Workspace Configuration

After completing this guide, you will have the necessary [Workspace
configuration](../../../reference/configuration/#workspace-configuration) for Kelda to
boot your services in the development environment. Once you have this
foundation, you will be able to write the [Sync
configuration](../../sync/overview) to deploy local versions of
services.

## Background

The Workspace configuration has two parts:

1. The Kubernetes YAML (e.g. Deployments and Services) that deploy your application.
1. A `workspace.yaml` file that describes what tunnels should be created in the
   development environment.

When Kelda boots, it initializes the development environment by:

1. Reading all the Kubernetes YAML for the development environment from the Workspace directory.
1. [Assigning](../../../reference/configuration/#service-names) each Pod in the
   YAML a name that can be referenced in the rest of your configuration files.
1. Creating tunnels between the local machine and remote cluster according to
   the tunnels in the `workspace.yaml` file.

### Example `workspace.yaml`

The following `workspace.yaml` file is from the [Node Todo example
application](https://github.com/kelda-inc/examples/tree/master/node-todo/kelda-workspace).

For more explanation on the `workspace.yaml` format see the docs
[here](../../../reference/configuration/#workspace-configuration).

    # The version of the configuration format. This doesn't need to be changed.
    version: "v1alpha1"

    # Requests to localhost:8080 will get forwarded to port 8080 in the
    # web-server service.
    # The "web-server" service refers to the pod in the "web-server-dep.yaml"
    # file in the Example Directory Structure.
    tunnels:
    - serviceName: "web-server"
      remotePort: 8080
      localPort: 8080

### Example Directory Structure

The [Node Todo example application](https://github.com/kelda-inc/examples/tree/master/node-todo/kelda-workspace)
has the following directory structure. Note how there's a `workspace.yaml`
file, along with Kubernetes files for the mongodb and web-server microservices.

    ├── workspace.yaml
    ├── mongodb-secret.yaml
    ├── mongodb-statefulset.yaml
    ├── mongodb-svc.yaml
    ├── web-server-dep.yaml
    └── web-server-svc.yaml

If necessary, you can group your YAML in [more advanced
ways](../../../reference/configuration/#workspace-configuration).

## Instructions

1. Create the basic directory structure for your workspace configuration.

        mkdir kelda-workspace
        echo 'version: "v1alpha1"' > kelda-workspace/workspace.yaml

1. Gather your Kubernetes YAML used for deploying the services.

    * If you have production Kubernetes YAML, then you're good to go.
    * If you're using Helm, place the Helm chart in the workspace directory
      according to [these instructions](../helm).
    * If you're using a different templating tool, you can setup Kelda to
      [deploy the output of a script](../templating-tools).
    * If you're using Docker Compose, use
    [`kompose`](https://github.com/kubernetes/kompose) to convert your
    `docker-compose.yml` into Kubernetes YAML.

    ??? note "Special handling for Docker Compose volumes"
        Edit the Docker Compose file so that it doesn't have Docker-specific
        volume configuration since this is handled differently in Kubernetes.

        * `volumes_from` for data containers should use the `InitContainer` and
          shared volume approach described
          [here](../updating-kube-yaml-for-dev/#data-volumes).
        * All syncing should use Kelda's [Sync configuration](../../../reference/configuration/#sync-configuration)
          to define which files need to be synced.

1. Copy your Kubernetes YAML into the `kelda-workspace` directory you made in step 1.

1. Modify your Kubernetes YAML to be suitable for the development environment.

    Although any Kubernetes YAML can be used with Kelda, production deployments
    often need to be tweaked to be suitable for development environments. See
    [this guide](../updating-kube-yaml-for-dev/) for
    some common changes.

1. If your Kubernetes YAML references private Docker images, grant Kelda access
   to pull them by following the instructions
   [here](../private-images/).

1. Point your user configuration at your new workspace.

    In the `kelda-workspace` directory that you created in step 1, run

        kelda config

    and answer the questions. This will update your `~/.kelda.yaml` file so
    that the `workspace` field points at your newly created Workspace
    configuration.

1. Test your Kubernetes YAML by deploying it with Kelda.

    The following command will deploy all the services in the `workspace.yaml`
    file. We're using the `--no-sync` flag because we haven't written any
    [Sync configuration](../../sync/overview) yet to tell
    Kelda what files to sync.

        kelda dev --no-sync

    If all goes well, your services should enter the Ready state. If not,
    follow the next step to fix your Kubernetes YAML.

1. Modify your Kubernetes YAML until your application behaves as expected.

    To debug Kubernetes issues, access the cluster directly with

        kubectl --namespace <namespace in ~/.kelda.yaml>

    You will need to re-run `kelda dev` in order to deploy modifications to your
    YAML.

1. Add tunnels to your `workspace.yaml` file. These can be used to test your
   application.

        tunnels:
        - serviceName: <name from 'kelda dev' output>
          remotePort: <port in container>
          localPort: <port on localhost>

    You will need to re-run `kelda dev` in order to deploy modifications to
    your `workspace.yaml`.
