# Quickstart

If this is your first time using Kelda, we recommend trying out the [example
app](../experience-kelda) or watching the [demo video](/docs).

Once you're ready to start using Kelda with your own application, you'll need
to [deploy](../deployment) a private version of Kelda, and write a small amount
of [configuration](../configuring-kelda/overview).

---

## **Step 1: Install the Kelda CLI**

Run the following command to download and install Kelda.

    curl -fsSL 'https://kelda.io/install.sh' | sh

---

## **Step 2: Try out an example application**

In **5 minutes**, experience what it's like to develop with Kelda by running
one of our example applications on our demo cluster. You'll also learn how to
configure Kelda for different applications.

We recommend starting with [Magda (Node.js)](../experience-kelda), but we have
examples for [Python](../example-apps/polls) and
[Golang](../example-apps/sock-shop) as well.

---

## **Step 3: Deploy a private version of Kelda**

??? tip "Don't want to create a Kubernetes cluster? Try out [Hosted Kelda](/request-hosted-kelda-access) and skip straight to step 4."

The public demo cluster is great for playing around with Kelda, but you'll need
your own Kelda cluster to work on private code.

Our [deployment guide](../deployment) will get you setup with a private
instance of Kelda on your own Kubernetes cluster. Once you've setup your
cluster, you'll be ready to setup the
[configuration](../configuring-kelda/overview) needed to use your app with
Kelda.

---

## **Step 4: Configure Kelda for your own application**

Kelda needs a small amount of configuration in order to boot your services,
and sync your local changes.

Once you finish our [configuration guide](../configuring-kelda/overview),
you'll be all set to start using Kelda for testing your local code changes.
