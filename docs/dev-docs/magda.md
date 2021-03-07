# Magda

Magda is our go-to project for testing and demos. To test with Magda:

```
# Clone the Kelda config for Magda.
$ git clone git@github.com:kelda-inc/magda-kelda-config
```

```
# Modify your ~/.kelda.yaml to point at the workspace.yaml in magda-kelda-config.
$ vim ~/.kelda.yaml
```

```
# Develop the application.
$ git clone git@github.com:kelda-inc/magda-web-server
$ cd magda-web-server
$ kelda dev
```

A simple test is editing the handler for "/server-config.js" in dist/index.js.
Modify the returned string, and check that Kelda deployed the change by
fetching localhost:8080/server-config.js.
