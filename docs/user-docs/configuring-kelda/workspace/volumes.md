# Persisting State with Volumes

Because Kelda can deploy all standard Kubernetes objects, [Kubernetes
Volumes](https://kubernetes.io/docs/concepts/storage/volumes/) work out of the box.

To add a volume to your development environment, just modify the Kubernetes
YAML in your [Workspace
configuration](../../../reference/configuration/#workspace-configuration) to
reference the volume.

## Persisting State Across Pod Restarts

??? warning "Persistent Volumes don't persist state across calls to `kelda delete`"

    The storage device associated with the PVC is released once the PVC is
    deleted, and Kelda deletes all objects in the namespace when `kelda delete`
    is called.

    If you're interested in persisting state across calls to `kelda delete`
    [let us know](/contact)!

To persist state across pod restarts, use a [Persistent
Volume](https://kubernetes.io/docs/concepts/storage/persistent-volumes/). The
easiest way to create a persistent volume is with a [Persistent Volume
Claim](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims) (PVC).

A PVC tells Kubernetes to allocate a Persistent Volume for you. The contents of
this volume will be retained even if the pod is deleted. Once a PVC is created,
pods can mount it into their filesystem.

[This article](https://portworx.com/basic-guide-kubernetes-storage/) has a
great explanation of how Volumes work in Kubernetes, but in most cases you can
just follow one of the examples below.

??? note "If PVCs aren't supported on your Kubernetes cluster..."

    If PVCs aren't supported on your Kubernetes cluster, you can still use
    Persistent Volumes. You'll just need to reference the volume directly from
    your pod spec.

## Example: Persisting Database State

[This example Todo
application](https://github.com/kelda-inc/examples/tree/master/node-todo-with-volumes)
persists the `/data/db` directory in the Mongo database across pod restarts. If
you force a pod restart with `kubectl delete pod -n <namespace> <pod name>`,
the database entries will still be present when Mongo boots back up.

All the YAML required to setup the volume is in the [Workspace configuration](https://github.com/kelda-inc/examples/tree/master/node-todo-with-volumes/kelda-workspace/mongodb).

The [mongodb-pvc.yaml](https://github.com/kelda-inc/examples/blob/master/node-todo-with-volumes/kelda-workspace/mongodb/mongodb-pvc.yaml)
file defines the PVC. This creates a Volume that can be referenced in our Pod.

    kind: PersistentVolumeClaim
    apiVersion: v1
    metadata:
     name: mongo-pvc
    spec:
     accessModes:
      - ReadWriteOnce
     resources:
      requests:
       storage: 1Gi

The Pod [references the
volume](https://github.com/kelda-inc/examples/blob/master/node-todo-with-volumes/kelda-workspace/mongodb/mongodb-statefulset.yaml#L31):

    volumes:
    - name: data-volume
      persistentVolumeClaim:
        claimName: mongo-pvc

And [mounts
it](https://github.com/kelda-inc/examples/blob/master/node-todo-with-volumes/kelda-workspace/mongodb/mongodb-statefulset.yaml#L25)
into the container:

    volumeMounts:
    - name: data-volume
      mountPath: /data/db

## Resetting Your Volume

If you need to remove the files in your volume, delete and recreate it by
running `kelda delete` followed by `kelda dev`.

You can also manually delete files by getting a shell in the container with
`kelda ssh`, and directly `rm`ing files in the volume.
