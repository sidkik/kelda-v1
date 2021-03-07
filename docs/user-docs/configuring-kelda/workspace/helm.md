# Specifying Your Deployment YAML with Helm

By default, the [Workspace configuration](../../../reference/configuration/#workspace-configuration)
deploys services with raw Kubernetes YAML.

If the directory contains a `Chart.yaml` file, Kelda will treat the directory
as a [Helm](https://helm.sh/) chart, and automatically convert it into raw Kubernetes YAML when
deploying.

By default, Kelda uses the `values.yaml` file located in the same directory as
the `Chart.yaml`.

## Example

The [gateway
service](https://github.com/kelda-inc/examples/tree/master/magda/magda-kelda-config/gateway)
in the Magda example uses a Helm chart.

Its directory structure looks like this:

    gateway
    ├── Chart.yaml
    ├── templates
    │   ├── configmap-gateway-config.yaml
    │   └── deployment-gateway.yaml
    └── values.yaml

Because it contains a `Chart.yaml` file, Kelda treats the entire directory as a
Helm chart, and renders the template according with the `values.yaml` file.

It then deploys the rendered Kubernetes YAML as if the YAML were specified directly.

## Limitations

Kelda uses `helm template` to convert Helm charts into raw Kubernetes YAML.

Because Kelda doesn't use Tiller to deploy the Helm chart, certain features
such as [lifecycle
hooks](https://helm.sh/docs/charts_hooks/#hooks-and-the-release-lifecycle)
aren't supported.
