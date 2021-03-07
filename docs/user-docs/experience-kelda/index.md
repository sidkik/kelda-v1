# Experience Kelda in 3 steps

Create a development environment and implement a fix in 5 minutes, all while
running your services remotely.

---

## **Step 1: Install Kelda**

    $ curl -fsSL 'https://kelda.io/install.sh' | sh

---

## **Step 2: Run Kelda**

The following command downloads and boots [Magda](https://data.gov.au), a
microservice application developed by the Australian government. The services
are deployed to the demo [Hosted Kelda](/request-hosted-kelda-access) cluster.

    $ kelda dev --demo

??? warning "The `kelda dev` process should not be killed during development"

    It's needed to sync local changes, and to keep the tunnels open.

    If you accidentally kill it, you can restart it by re-running the `kelda dev --demo` command.

<script id="asciicast-uT2DMR5H7U3uWatyhqsHJGCf2" src="https://asciinema.org/a/uT2DMR5H7U3uWatyhqsHJGCf2.js" async></script>

---

## **Step 3: Explore Kelda**

The images below step through fixing an example bug. We also have [step by step instructions](./explore).

<iframe width="800" height="800" class="slideshow" src="./slideshow.html" frameborder=0></iframe>

---

## **What's Next?**

If you're interested in using Kelda for your own application, you should [deploy
Kelda](../deployment) on your own infrastructure next.
