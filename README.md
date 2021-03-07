# Kelda

Kelda creates easy to use development environments for microservices.

## Setup

Kelda runs in a Kubernetes cluster. It does not create the cluster itself.

Kelda has two components:
- The **Minion** is a pod that runs in the cluster. It's responsible for
  creating and managing development namespaces.
- The **CLI** is how developers interact with their development namespace. It
  should be installed on the developer's laptop.

### Minion

1. Deploy the Minion pod

   ```
   $ kelda setup-minion --license <path to license>
   ```

2. Check that it booted:

   ```
   $ kubectl logs -l service=kelda -n kelda
   ```

### CLI

Install the CLI binary into your PATH

```
$ cp ./bin/kelda /usr/local/bin
```

### Configuration Files

#### kubeconfig

Kelda uses the credentials stored in `~/.kube/config` for communicating with
the development cluster. The user is expected to setup their `kubeconfig`
themselves.

#### User Configuration

The developer's laptop requires a configuration file in `~/.kelda.yaml`.
```
# A unique namespace that will be used for the development namespace.
namespace: kevin

# The Kubernetes cluster to use for development. The name of the current
# cluster can be retrieved through `kubectl config current-context`.
context: dev-cluster

# The path to the configuration for deploying the application.
# More explanation on the workspace.yaml is below.
workspace: ~/kelda-workspace/workspace.yaml
```

To setup the user configuration:
1. Make sure the current Kubernetes context is set to the development cluster.
2. `cd` into the Kelda workspace directory.
3. Run `kelda config`.

#### Workspace Configuration

The `workspace.yaml` specifies what services and tunnels should be created in
the development namespace.

```
tunnels:
  - serviceName: "gateway"
    remotePort: 80
    localPort: 8080

services:
  - name: "authorization-api"
  - name: "web-server"
  - name: "content-api"
```

The Kubernetes YAML for each service is stored in a directory with the same
name.  This YAML is deployed in each development namespace.

```
├── workspace.yaml
├── authorization-api
│   ├── deployment-authorization-api.yaml
│   └── service-authorization-api.yaml
├── content-api
│   ├── configmap-scss-compiler-config.yaml
│   ├── deployment-content-api.yaml
│   └── service-content-api.yaml
└── web-server
    ├── configmap-web-app-config.yaml
    ├── deployment-web.yaml
    └── service-web.yaml
```

#### Registry Credentials

The `regcred` secret in the `kelda` namespace is automatically copied to each
development namespace. Kubernetes manifests can then reference this secret as an
`ImagePullSecret`.

```
$ kubectl create secret docker-registry regcred -n kelda \
    --docker-server https://index.docker.io/v1/ \
    --docker-username=USERNAME \
    --docker-password=PASSWORD \
    --docker-email=EMAIL
```

#### Service Development Configuration

The directory from which `kelda dev` is run must have a configuration file
named `kelda.yaml` that specifies how to run the service when in development
mode.

```
# REQUIRED. Name of the service. Must match the name in workspace.yaml.
name: SERVICE_NAME

# REQUIRED. The files to sync from the local machine to the container.
sync:
- from: SRC_ON_LAPTOP
  to: DST_IN_CONTAINER
  except: ['.git']

# OPTIONAL. The image to run.
# If not set, Kelda uses the same image as when not developing.
image: DEV_IMAGE

# OPTIONAL. The command to run in the development container.
# If not set, Kelda uses the same command as when not developing.
command: DEV_COMMAND
```

##### File Sync Limitations

Kelda opens a file descriptor for each directory/subdirectory that's being
synced, so the number of files that can be synced are limited by the system's
maximum number of open files. If the limit is too low, `kelda dev` will error
with "too many open files in system".

On OSX, the default maximum number of open files is 256. This can be increased
by editing the `limit.maxfiles`:
https://medium.com/mindful-technology/too-many-open-files-limit-ulimit-on-mac-os-x-add0f1bfddde.

## Usage

### kelda dev

`kelda dev` is the main process. It...
- Creates the development namespace.
- Is the main status view of the development namespace.
- Syncs local code changes to the development namespace.
- Keeps tunnels open for accessing the application.

Run it in the directory containing the `kelda.yaml` [sync config](#sync-configuration).

### Debugging

`kelda ssh <SERVICE>` creates a shell in SERVICE.

`kelda logs <SERVICE>` shows the logs of SERVICE.

### Cleaning Up

`kelda delete` deletes the developer's namespace.

## Upgrading

1. Download and install the latest version of the `kelda` CLI

2. Deploy the new version of the Kelda minion with

   ```
   $ kelda setup-minion --license <path to license>
   ```

3. Delete old development namespaces created by the previous version

   ```
   $ kelda delete
   ```

   **Note** This will only affect the current user's namespace. Each developer must
   run `kelda delete`.

4. Resume development
   ```
   $ kelda dev
   ```
