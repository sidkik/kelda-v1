# Troubleshooting Common Issues

## Too Many Files to Watch for Changes

Kelda opens a file descriptor for each file that's being watched, so the number
of files that can be watched are limited by the system's maximum number of open
files. If the limit is too low, `kelda dev` will print a warning message when
starting.

Note that Kelda automatically increases the soft limit to the hard limit. To
increase the hard limit **temporarily**, use `ulimit`. For example, to set it
to 4096, run:

    ulimit -H -n 4096

To increase the limit **permanently**, edit `limit.maxfiles` as described in [this
blog post](https://medium.com/mindful-technology/too-many-open-files-limit-ulimit-on-mac-os-x-add0f1bfddde).

### Excluding Files from Syncing

If you're still running into the open files warning, it's possible that you're
syncing over files that aren't necessary (e.g. `.git`, or `node_modules`).

The `except` field can be used to exclude files from syncing. See the
[configuration documentation](../reference/configuration#sync) for more information.

## Node Ports

Using Kubernetes services with static `NodePort`s is not recommended because
the number of concurrent development sessions will be limited by the number of
nodes in the cluster. Each development session will bind to the port on a node,
and the cluster will eventually run out of nodes with an unused instance of the
port.

You have this problem if you get the following error:

    Service "gateway" is invalid: spec.ports[0].nodePort: Invalid value: 30000: provided port is already allocated

## Services with Multiple Pods
`kelda logs`, `kelda ssh`, and `kelda dev` currently only work for services
that have exactly one pod. Services with more than one pod can still be
deployed, but these commands will not function correctly.

```
  Service defines multiple pods, which isn't supported by Kelda. To resolve, either:
- Contact your administrator to split the service up
- Or use `kubectl` directly.
```
