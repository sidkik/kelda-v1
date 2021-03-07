# Sync Configuration

## Background

Kelda requires a [configuration
file](../../../reference/configuration/#sync-configuration) in order to know how
files should be synced from the local machine to the development cluster.

The file should be in the root directory of the service that's being developed.

### Example File

The [Node Todo
application](https://github.com/kelda-inc/examples/tree/master/node-todo) has
the following `kelda.yaml`. It syncs all
the files in the current directory to the remote container, and restarts the
Node service after each sync. Additionally, when the `package-lock.json` file
is synced, it runs `npm install` in order to update the dependencies in the
remote container.

    # The version of the configuration format. Only v1alpha1 is currently supported.
    version: "v1alpha1"

    # Name of the service. Must match the service name shown in 'kelda dev'.
    name: "web-server"

    # The command that gets run after each sync.
    command: ["node", "/usr/src/app/server.js"]

    # The command that gets run when there's a file change that matches a
    # sync rule with  "triggerInit" set to true.
    initCommand: ["npm", "install"]

    sync:

    # When the package-lock.json changes, run "npm install" in order to use the
    # new dependencies.
    - from: "package-lock.json"
      to: "/usr/src/app/package-lock.json"
      triggerInit: true

    # When any other change happens, restart the process with the new code.
    # Don't sync the files in the .git and node_modules directories to avoid
    # watching too many files.
    - from: "."
      to: "/usr/src/app"
      except: [".git", "node_modules"]
      triggerInit: false

## Instructions

You should set up the sync configuration after you've [set up your workspace
configuration](../../workspace/overview).

1. Decide what files you want to sync to the remote container. See
   [below](#deciding-what-to-synchronize) for common approaches.

1. Decide what command you want to run after each sync.

1. Create a `kelda.yaml` file in the root directory of the service you'd like to
   develop.

        version: "v1alpha1"
        name: "<name of service>"
        command: ["<command>", "<and>", "<arguments>"]
        sync:
        - from: "<local path>"
          to: "<path in container>"

    For more advanced configuration, see the [full list of configuration
    options](../../../reference/configuration#sync-configuration).

1. Start Kelda with the sync configuration that you just wrote.

        kelda dev <path to service directory>

1. Make a code change, and make sure that Kelda prints out that the file was synced.

1. Check the logs of the service under development to see that its process restarted.

        kelda logs <service name>

You can create additional `kelda.yaml` files for other services as you migrate
them to Kelda.

## Deciding What to Synchronize

When syncing code changes you can choose to compile your code on your local workstation
and sync the resulting files and binaries directly to Kubernetes, or sync the code changes to
Kubernetes and have the compilation step occur there. There are some tradeoffs to both
approaches. For the best experience we recommend the following:

* For interpreted languages, you should synchronize the source files, and install the dependencies
  in the remote container to avoid platform issues.
* For JVM languages, you should build the JAR locally and sync the result over. Kelda will automatically
  sync the artifacts, but triggering the compilation step it outside of the scope of Kelda.
* For other compiled languages, you should cross-compile locally. Compiling in the remote container
  is also an option, but it has not been thoroughly tested.
