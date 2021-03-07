# Upgrading

This document, intended for Kubernetes administrators, will walk you through
upgrading Kelda on your Kubernetes cluster.

1. **Download the latest release of Kelda**

    Paste the following into your shell to download the latest Kelda release:

        curl -fsSL 'https://kelda.io/install.sh' | sh

    You should see the following output:

        Downloading the latest Kelda release...
        ######################################################################## 100.0%

        The latest Kelda release has been downloaded to the current working directory.

        Copy the binary into /usr/local/bin? (y/N) y
        You may be prompted for your sudo password in order to write to /usr/local/bin.
        Password:


    Verify that you have correctly installed the latest version of the Kelda CLI.

        kelda version

        local version:  0.11.0
        minion version: 0.10.0

1. **Upgrade the Kelda minion**

    Deploy the latest version of the Kelda minion to your Kubernetes cluster
    with the following command:

        kelda setup-minion

    The output should look something like this:

        Deploy to context `dev`? (y/N) y
        Deploying Kelda components to the `dev` context....
        Waiting for minion to boot....
        Done!

1. **Verify the new version**

    You can verify the new version by running `kelda version`. The output of
    this command will look something like this:

        kelda version

        local version:  0.11.0
        minion version: 0.11.0

1. **Delete previous namespace**

    You must delete your development namespace whenever you upgrade to a new
    version of the CLI.

        kelda delete

    The output of this command will look something like this:

        Deleting namespace 'user-namespace'.........................

    Executing this command will find the namespace for the current user and
    delete it. This will take a few minutes.

    **Note:** This will only affect the current user's namespace. Each
    developer must run `kelda delete`.

1. **Resume development**

    You can resume development by running `kelda dev`. This will create a new
    namespace using the latest version of Kelda.
