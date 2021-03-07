# Release Instructions

## Releasing a New Kelda Version

1. Update the [Release Notes](../user-docs/Administrator/upgrading.md) for the new version.
2. Merge the Release Notes to `master`.
3. Make a tag with the version of the release.
```
$ git tag 0.9.0 && git push upstream 0.9.0
```
4. Circle will take care of it from here. When it's done, the release will be
   in both the S3 [kelda-releases](https://s3.console.aws.amazon.com/s3/buckets/kelda-releases/) bucket, and the Google Drive [Kelda Releases](https://drive.google.com/drive/folders/12799o1EHbbbaNk1mGXqfeBxPdlQY5Phr)
   folder.

## Onboarding a New Customer

Each customer requires a unique license. Generate the license with
```
$ go run ./scripts/make-license -company <COMPANY>
```
and send the generated `kelda-license` file to the customer.

## Revoking a License

Run `./scripts/revoke-license.sh <COMPANY>`.
