# Integrating With Templating Tools

The `script` field can be used to integrate with templating tools such as
`kustomize`.

When specified, Kelda runs the script when booting, and deploys the output of
the script. The following example runs the `kustomize build` command before
deploying the output.

Note: If the `name` field is absent, the service name is the name of the 
    deployment object from the output of the `script` field.

```
services:
  - name: "hello"
    script: ["kustomize", "build", "./overlays/development"]
```

See here for [the full kustomize example](
https://github.com/kelda-inc/examples/blob/master/kustomize/kelda-config/workspace.yaml#L4).
The example doesn't have any development services, so run it with `kelda dev
--no-sync`.

### Environment Variables

The following environment variables are available to the commands specified by the `script` field:

- KELDA_NAMESPACE: The user's Kelda namespace from the [user config](https://kelda.io/docs/reference/configuration/#namespace). 

## **Helm**

We also have first class support for [Helm](../helm).
