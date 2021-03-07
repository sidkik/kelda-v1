# Ignoring Files

By default, Kelda attempts to deploy all `.yaml` files in the Workspace. You
can configure Kelda to skip deploying certain YAML files with the `ignore`
field.

The following Workspace config will deploy all YAML files except those named
`codefresh.yaml`. It will also ignore instances of `codefresh.yaml` in
subdirectories (e.g. `web/codefresh.yaml`).

```
version: "v1alpha1"
ignore:
  - "codefresh.yaml"
```
