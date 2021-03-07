# Writing Docs

## Previewing Changes

We use [mkdocs](https://www.mkdocs.org) for our documentation.

To preview the docs:
```
$ make docs
```

The dependencies will be installed in a Python virtual environment and an mkdocs server will start running on `localhost:8000`

For each merge to master, and each release, the docs are automatically pushed to
http://kelda-docs-${VERSION}.s3-website-us-west-1.amazonaws.com.

## Release Notes

Users refer to the Release Notes in the [Upgrade
Guide](../user-docs/Administrator/upgrading.md) when deciding whether and how
to upgrade.

Commits that introduce new features, or make breaking changes, should update
the Release Notes in the same commit.

Bug fixes do not need an entry in the Release Notes.

## Links

All links should be relative links since the docs are deployed to `kelda.io/docs`.
Absolute links would resolve to relative to `kelda.io` (e.g. `/reference` would
resolve to `kelda.io/reference` rather than `kelda.io/reference`).
