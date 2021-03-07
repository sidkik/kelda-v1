# Configuration

## Workspace Configuration

The `workspace.yaml` file specifies what tunnels should be
created in the development namespace.

Kelda also discovers the Kubernetes YAML files to deploy by searching in the
Workspace directory.

    # Sample workspace directory structure.
    # The workspace.yaml file defines what tunnels to use, and the other
    # Kubernetes YAML files are deployed into the development environment.

    ├── workspace.yaml
    ├── mongodb-secret.yaml
    ├── mongodb-statefulset.yaml
    ├── mongodb-svc.yaml
    ├── web-server-dep.yaml
    └── web-server-svc.yaml

---
    # Sample workspace.yaml file.
    version: "v1alpha1"

    # Requests to localhost:8080 will get forwarded to port 8080 in the
    # web-server service.
    # The "web-server" service refers to the pod in the "web-server-dep.yaml"
    # file in the Sample Directory Structure.
    tunnels:
    - serviceName: "gateway"
      remotePort: 80
      localPort: 8080


To keep things organized, you can also sort the Kubernetes YAML into
directories. Kelda recursively searches for all YAML files.

    ├── workspace.yaml
    ├── mongodb
    │   ├── mongodb-secret.yaml
    │   ├── mongodb-statefulset.yaml
    │   └── mongodb-svc.yaml
    └── web-server
        ├── web-server-dep.yaml
        └── web-server-svc.yaml

### Fields

#### tunnels _(optional)_

    tunnels:
    - serviceName: "gateway"
      remotePort: 80
      localPort: 8080

A list of `tunnel` objects for accessing remote services directly from your
local machine.

In the example above, we are proxying port 80 on the `gateway` service to port
8080 on our local workstation.

The service name "gateway" was [automatically generated](#service-names) based
on the Kubernetes YAML that's being deployed. To find out what name to use, run
`kelda dev --no-sync`.

The `serviceName`, `localPort`, and `remotePort` fields are required.

#### services _(optional)_

    services:
    - name: "authorization-api"
    - name: "content-api"
    - name: "web-server"

An optional list of directories to deploy. If this field is set, then only the
YAML in the directories corresponding to the service name are deployed.

For example, the above services might have the following directory structure.

    ├── workspace.yaml
    ├── authorization-api
    │   └── deployment.yaml
    ├── content-api
    │   └── deployment.yaml
    └── web-server
        └── deployment.yaml

#### version

The version of the configuration file format. Only `v1alpha1` is currently
supported.

## Registry Credentials

See our documentation on [private
images](../../configuring-kelda/workspace/private-images) for information on
how to setup a registry credential that lets Kelda pull private images.

## User Configuration

Each developer's workstation requires a configuration file located at
`~/.kelda.yaml`.

    # The version of the configuration format.
    version: v1alpha1

    # A unique namespace that will be used for the development namespace.
    namespace: user

    # The Kubernetes cluster to use for development. The name of the current
    # cluster can be retrieved through `kubectl config current-context`.
    context: dev-cluster

    # The path to the configuration for deploying the application.
    # More explanation on the workspace.yaml is below.
    workspace: ~/kelda-workspace/workspace.yaml

### Generation

This configuration can be generated using the Kelda CLI.

1. Ensure that the current Kubernetes context is set to the development
   cluster. You can verify this with `kubectl config current-context`.
2. `cd` into the Kelda workspace directory.
3. Run `kelda config`.

    The `config` command is used to set up a user-specific configuration for
    Kelda. Running this command will ask you a couple of questions.

    After completing these configuration prompts, a Kelda configuration file
    will be written to your home directory.

        Wrote config to /home/user/.kelda.yaml

### Fields

#### context

The Kubernetes context for the Kelda Kubernetes cluster. Kelda connects to the
remote cluster by looking up the cluster and authentication information for
this context in your local
[`kubeconfig`](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)
file.

#### namespace

The namespace in the Kubernetes cluster that the Kubernetes objects will be
deployed to.

This namespace is created and managed by Kelda, but you can also interact
directly with it with `kubectl`. For example, to get detailed information on
pods, run

    kubectl describe pods -n <namespace>

#### workspace

The path to the `workspace.yaml` file that describes what services should be
deployed to the development cluster.

#### version

The version of the configuration file format. Only `v1alpha1` is currently
supported.

## Sync Configuration

The directory from which `kelda dev` is run must have a configuration file
named `kelda.yaml` that specifies how to run the service in development mode.

    # REQUIRED. The version of the configuration format.
    version: "v1alpha1"

    # REQUIRED. Name of the service. Must match the service name shown in 'kelda dev'.
    name: SERVICE_NAME

    # REQUIRED. The files to sync from the local machine to the container.
    sync:
    - from: SRC_ON_LAPTOP
      to: DST_IN_CONTAINER
      except: ['.git']
    - from: SRC
      to: DST
      triggerInit: true

    # OPTIONAL. The image to run.
    # If not set, Kelda uses the same image as when not developing.
    image: DEV_IMAGE

    # OPTIONAL. The command to run in the development container after each file sync.
    # If not set, Kelda uses the same command as when not developing.
    command: ['DEV_COMMAND']

    # OPTIONAL. The command to run for sync rules that have `triggerInit` set
    # to true (e.g. the second rule above).
    initCommand: ['INIT_COMMAND']

### Fields

#### name

The name of the service. This must match the name [generated by
Kelda](#service-names) based on the Workspace configuration.

#### command

The main process run by the container. It is restarted after each file change.

Must be a list of strings.

#### initCommand

An optional command that can be triggered when certain files
are synced. After the `initCommand` completes successfully, the normal
[`command`](#command) is run.

Must be a list of strings.

#### sync

A list of sync rules that describe how files are synced from the local filesystem
to the remote container.

`from` and `to` are the paths in the local and remote container, and are required.

`except` is an optional list of paths to ignore within the `from` and `to`
paths. For example, the following sync rule ignores `local/node_modules`,
`remote/node_modules`, and `local/node_modules/express`.

    from: local
    to: remote
    except: ['node_modules']

`triggerInit` is an optional flag that causes the `initCommand` to be run
before the `command` is restarted. It defaults to `false`.

#### image

`image` is an optional override for the image to be used when running in
development mode. By default, Kelda uses the image in the Kubernetes object in
the [Workspace configuration](#workspace-configuration).

#### version

The version of the configuration file format. Only `v1alpha1` is currently
supported.

## Service Names

Kelda autogenerates service names so that services can be referenced in Kelda
configuration files. For example, a service name is needed to use for the [Sync
configuration](#sync-configuration) and tunnels, as well as the `kelda logs`
and `kelda ssh` commands.

Kelda uses the name of Kubernetes object to create the identifier.

The following Deployment would be named `web-server`.

    apiVersion: extensions/v1beta1
    kind: Deployment
    metadata:
      name: web-server
    spec:
      ...

If the service's configuration is in a directory, the directory's name is used
to construct the service name.
