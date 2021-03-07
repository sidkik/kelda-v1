# Release Notes

<!---
### Next Release

Notes for the upcoming release are added here when the relevant code is added,
and uncommented when the next release is made.
--->

### 0.15.14

* Gracefully handle error from removing files that don't exist when syncing.

### 0.15.13

* Gracefully update PVCs rather than deleting and recreating them.

### 0.15.12

* When a service has a `script` field but no `name` field, its name is the 
  name of the deployment level object from the output of the `script` field.

### 0.15.11


* Commands specified in the `script` field of the workspace YAML file now have
  access to the `KELDA_NAMESPACE` environment variable. 
* CRDs can now be deployed by Kelda. Using this change requires upgrading the
  Kelda Minion as well as the Kelda CLI.

### 0.15.10

Added a field to the Workspace config to ignore YAML files.

### 0.15.9

Fixed a bug with updating services with PVCs.

### 0.15.7

`kelda login` now requires a token for authentication.

### 0.15.6

Kelda now supports deploying the output of a script. This is helpful for
integrating with templating tools such as `kustomize`.

To use this, modify your Workspace configuration to use the `script`
field:

```
services:
  - name: "hello"
    script: ["kustomize", "build", "./overlays/development"]
```

See here for [the full kustomize example](
https://github.com/kelda-inc/examples/blob/master/kustomize/kelda-config/workspace.yaml#L4).
The example doesn't have any development services, so run it with `kelda dev
--no-sync`.

### 0.15.5

This release adds the `kelda dev --demo` flag to make it even easier to
experience Kelda.

### 0.15.4

This is the alpha release for [Hosted Kelda](https://kelda.io/request-hosted-kelda-access/). 
Hosted Kelda lets you dive into developing with Kelda without bothering with
creating a Kubernetes cluster yourself.

### 0.15.3

* Allow upgrading from 0.14.0 without redeploying namespaces.

### 0.15.2

* Make it easier to connect to the Kelda demo cluster. You now just need to set
  your `context` in `~/.kelda.yaml` to `kelda-demo-cluster`.

### 0.15.1

* Gracefully update Service objects. Before, we would delete and recreate the
  object, which would cause the service's cluster IP to change.

* You no longer need a license in order to run Kelda in trial mode (one
  developer per cluster).

### 0.15.0

* Enable compression to the minion.<br/><br/>
  This is an **API breaking change**, so the minion must be
  [upgraded](../reference/upgrading/) when the CLI is updated.

* Change our syncing algorithm to track individual files rather than directories
  containing files. Syncing to a directory that already contains files now
  ignores any pre-existing files, rather than removing them.

* Changing the sync command without changing any files now triggers the new
  command to be run.

* Changes to file modification time and permissions are now synced. If you need
  to restart the remote process, you can now just run `touch <tracked file>`.

* Files synced to an existing directory are now placed within the directory.
  For example, the sync rule `{from: "index.js", to: "/"}` applied to
  `index.js` syncs the file to `/index.js`.

* You can now just drop your Kube YAML into your Workspace configuration
  directory without grouping them into services first. If no services are
  specified in the `workspace.yaml`, we now deploy all the YAML in the
  Workspace directory.

### 0.14.4

* `kelda logs` now supports getting logs for multiple services at once.

<script id="asciicast-ELKJA9tNqtUtX9YZAChipgoFk" src="https://asciinema.org/a/ELKJA9tNqtUtX9YZAChipgoFk.js" async></script>

### 0.14.3

* Fixed a bug where Kelda would watch all files in the parent directory when
  syncing a file. This resulted in permission issues if there was a file in the
  directory with `000` permissions.
* Pre-emptively error when Kelda configuration is malformed.
* Prompt users to update CLI when it's incompatible with the Minion.

### 0.14.2

* Support Helm charts in Workspace configuration.
* Automatically increase file watching limit. This fixes the "too many open
  files" error in most cases.

### 0.14.1

* Add the `kelda config get-context` and `kelda config get-namespace` commands,
  which are helpful for scripting.

### 0.14.0

* Make services under development accessible even if they fail their readiness
  checks.
* Don't prompt for known values when running the Kelda demo.

### 0.13.7

This release makes various bug fixes to support syncing and running executable
files.

### 0.13.3

This release makes it easier to try out Kelda. It introduces the qk8s script to
spin up Kubernetes clusters, and makes various improvements to the commands run
as part of the quickstart.

It also reintroduces the namespace prioritization feature so that it's only
enabled on Kubernetes clusters that support the PriorityClass resource.

### 0.12.0

- Added `kelda upgrade-cli`, which makes it easy to upgrade the CLI to match
  the minion's version.
- Simplify the initial Kelda installation process. Kelda can now be installed
  via a shell script downloaded by `curl`.

### 0.11.0

- Added `kelda update`, which updates the container images in the
  development environment to the latest versions available upstream.
- Added support for "init commands" during file syncs. These are commands that
  are only triggered when certain files are changed. For example, this can be
  used to only run `npm install` when `package-lock.json` is changed.

### 0.10.0

- We've changed the way we transmit errors between the minion and CLI. This is
  a **breaking change** and requires updating both the CLI and Minion.
- We now require a license file to install Kelda, and automatically collect
  usage analytics.

### 0.9.1

- Fix bug where changes to Kubernetes manifests wouldn't get deployed.

### 0.9.0

Release 0.9.0 makes it easier to install the Minion, and makes some UX
improvements around error handling.

### 0.8.0

The first release using our new versioning scheme.
