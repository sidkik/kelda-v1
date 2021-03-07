# Writing the Kelda Configuration

This guide describes how to set up the Kelda configuration required to deploy
your application.

For an overview of Kelda's configuration model, see the docs [here](../../reference/configuration).

## Required Configuration

Two types of configuration are required in order to use Kelda:

1. The [**Workspace**](../../configuring-kelda/workspace/overview/)
   configuration, which contains the Kubernetes YAML for deploying all the
   services in the development environment.
1. The [**Sync**](../../configuring-kelda/sync/overview/)
   configuration, which defines how local code should be synced to the remote
   cluster during development.

Before using Kelda, you must set up the Sync configuration for at
least one service, and the Workspace configuration for all services
that it depends on.

## Approach

We recommend the following approach when configuring a new application.

1. Pick the first service that you want to develop with Kelda.
1. Follow the [guide on writing the Workspace
   configuration](../../configuring-kelda/workspace/overview/). This configuration
   contains the Kubernetes YAML for deploying the services into the development
   environment.
1. Follow the [guide on writing the Sync
   configuration](../../configuring-kelda/sync/overview/) to enable development
   on your first service.
1. At this point, you can start using Kelda to develop your first service. You
   can write the Sync configuration for other services as necessary.
