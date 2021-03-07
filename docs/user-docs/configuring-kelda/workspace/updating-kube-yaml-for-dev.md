# Updating Kubernetes YAML for Development

## Decrease Scale

Most of the Kelda features only work when there is exactly one container per
service. To meet this requirement, you must:

* Decrease the number of replicas in your Deployments, StatefulSets, etc.
* Disable autoscaling.

## Remove Unnecessary Services

Remove services that do not make sense to run in the development environment
such as scheduled tasks.

## Remove Publicly Exposed Services

If you have services that are publicly exposed in production (e.g. via a
LoadBalancer Service), you probably want to use Kelda
[tunnels](../../../reference/configuration/#tunnels) to expose them during development.

## Remove Node Ports

Using Kubernetes services with static `NodePort`s is not recommended because
the number of concurrent development sessions will be limited by the number of
nodes in the cluster. Each development session will bind to the port on a node,
and the cluster will eventually run out of nodes with an unused instance of the
port.

You have this problem if you get the following error:

    Service "gateway" is invalid: spec.ports[0].nodePort: Invalid value: 30000: provided port is already allocated

## Cloud Services

If you depend on cloud services in production, such as Amazon RDS, we suggest
using a containerized version in your development environment. Although it's
possible to connect directly to the cloud service, using a containerized
version makes it easy to enforce isolation between developers.

## Data Volumes

A common pattern is to run mock copies of databases in the development
environment. We suggest building a container with mocked data and deploying it
to the development cluster. A sample Kube YAML file that follows this pattern
for elasticsearch is shown below:

    containers:
    - image: elasticsearch:1
    name: elasticsearch
    volumeMounts:
    - mountPath: /usr/share/elasticsearch/data
        name: data
    initContainers:
    - image: data-es-geoservice:latest
    name: elasticsearch-data
    command: [ "sh", "-c", "cp -r /usr/share/elasticsearch/data/* /usr/share/elasticsearch/data_dst" ]
    volumeMounts:
    - mountPath: /usr/share/elasticsearch/data_dst
        name: data
    volumes:
    - name: data
        emptyDir: {}
