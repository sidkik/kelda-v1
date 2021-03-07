# Developing Kelda

## Cloning the repository

1. Ensure that `GOPATH` is set to your workspace location

2. Clone the Kelda repository to `$GOPATH/src/github.com/kelda-inc/kelda`

3. Build with `make install`. Ensure that `$GOPATH/bin` is in `PATH` if you want to launch Kelda without using the full path, but make sure this doesn't cause a conflict with other versions of Kelda e.g. in `/usr/local/bin`.

## Developing the CLI

1. Create a GKE cluster at https://console.cloud.google.com/kubernetes/list.

2. Connect to the cluster from your laptop. Install the [Google Cloud SDK](https://cloud.google.com/sdk/install), copy the connect command in the
   cluster UI and run it locally. For example, for me it was:
```
$ gcloud container clusters get-credentials kelda-dev --zone us-west2-a --project kklin-quilt-168120
```

   Test that the cluster is ready with `kubectl get nodes`.

3. Install the CLI.
```
$ make install
```

4. Deploy the Kelda minion.
```
$ kelda setup-minion --license ./scripts/internal-usage-license
```

## Developing the Minion and dev-server

1. Setup a Makefile override to push to your registry.
```
$ echo 'DOCKER_REPO = gcr.io/kevin-230505' >> local.mk
```

**Note**: You can ignore `local.mk` by adding it to `.git/info/exclude`

2. Compile and deploy
```
$ make -j 2 install docker-push && kelda setup-minion --force --license ./scripts/internal-usage-license
```

## Running the integration tests locally

1. Make sure that you have successfully executed the [Quickstart](../user-docs/index.md) and the steps in `Developing the Minion and dev-server`.

2. Setup a Makefile override to specify the paths to the examples repo.
```
$ echo 'KELDA_INC_PATH = $(GOPATH)/src/github.com/kelda-inc' >> local.mk
$ echo 'CI_EXAMPLES_REPO_PATH = $(KELDA_INC_PATH)/examples' >> local.mk
```

3. Run the integration tests.
```
$ make integration-test-local
```

## Integration Test Triage

Each week, a different team member is responsible for triaging integration test
failures. This person isn't responsible for fixing the issue, but they ensure
that the issue eventually gets resolved.

Some examples of appropriate responses to a test failure:
* Determining what feature caused the issue, and pinging the person who wrote
  the feature to fix it.
* Looking into what caused the issue, but finding that there's not enough
  information, and adding more debugging to the tests.
* Deciding with the team that we won't fix a particular test failure, and
  updating the tests to work around it.
* Fixing the root cause.

Every failure in the #integration-tests channel should have a response as a
thread so that other team members can quickly know the status of the issue.

This job is really important. It takes priority over all feature work.
Integration test failures are almost as important as customer issues -- if
something failed in integration testing, it will fail for a customer. By taking
every issue seriously, we make sure that no catchable issues are slipping
through to users.

## Upgrading Integration Tests Kubernetes Version

Currently, the integration tests run on 2 versions of Kubernetes clusters: the
oldest Kubernetes version we would like to support (currently 1.9.0) and the
newest Kubernetes release. When Kubernetes makes a new release, we need to
upgrade our integration tests so that it runs on the newer version. This can be
done by:

1. Replacing all the outdated Kubernetes version in `.circleci/config.yml` to
   the newest version. For example, replace all
   `kubernetes_version: "<old-ver>"` with `kubernetes_version: "<new-ver>"`. Do
   not touch `kubernetes_version: "1.9.0"`s as they are supposed to test Kelda
   on the minimum Kubernetes version we would like to support.
2. Deleting the old VPC network in project `kelda-ci` in GCP (such as the one
   named `kelda-ci-<old-ver-without-dot>`) and create a new VPC network named
   `kelda-ci-<new-ver-without-dot>`, such as `kelda-ci-1152` for Kubernetes
   1.15.2.
