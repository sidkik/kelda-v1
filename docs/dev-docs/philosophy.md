# Kelda Development Philosophy

## Testing

*Buggy code is extremely expensive. Tests catch bugs.*

*It's hard to know the effects of changing code. Tests catch unexpected changes in behavior.*

### Unit Tests

Unit tests assert that each method does what we want it to. Good unit tests:
- Cover both the happy and unhappy paths.
- Make it obvious to the reader that the code works.
- Catch changes in behavior when code is modified.

Bad unit tests increase coverage, but don't actually verify anything meaningful
about the code. A bad unit test is worse than no unit tests since it takes time
to write and maintain.

All new code should be unit-tested, within reason.

### Integration Tests

What tests would you run if a customer was about to use Kelda, and you wanted
to be certain everything would work? Automate those through integration tests.

Integration tests boot all the components of Kelda to simulate real usage.

All major features should be integration tested.

## Error Messages

*Error messages are for the user's benefit, not ours.*

Good error messages allow users to figure out what they need to do differently.

Bad error messages require consulting a Kelda developer so that they can trace
the error's code path, and divine the real reason for failure.

## Git Commits

Each commit should be a coherent whole that implements one idea completely and
correctly. No commit should ever break the code, even if another commit "fixes
it" later.

Good commit messages make Kelda easier to maintain, and unlock the power of
tools like `git revert`, `log`, and `blame`.

A good commit message:
- Has a subject line that summarizes the change.
- Uses the imperative mood in the subject line. It should fit the form "If
  applied, this commit will <SUBJECT>".
- Provides context on *what* and *why* (instead of *how*) in the body.
- Uses the form `<Area>: <Title>` in the subject line.  The title should be
  capitalized, but not end with a period.
- Limits the subject line to 50 characters.
- Wraps the body at 72 characters.

See https://chris.beams.io/posts/git-commit/ for more information.

## Code Review

We're all responsible for the quality of the overall project, not just for the
bits that we've written. Code review is an opportunity to spot potential issues
before they're merged.

All code must be reviewed before merging.
- For non-committers: Two approvals are required. First from a non-committer,
  then from a committer.
- For committers: One approval is required, either from a non-committer or
  committer.

## Versioning

We follow semantic versioning (`MAJOR.MINOR.PATCH`), but during the pre `1.0`
phase the version meanings are slightly different:
- The minor version is incremented for new features.
- The patch version is incremented for bug fixes.
- Minor versions aren't necessarily [compatible](#backwards-compatibility) with
  each other.
- We generally don't backport bug fixes, and instead prefer to help users
  upgrade to the latest version.

### Backwards Compatibility

We try to preserve backwards compatibility within one release. So the CLI and
cluster components should be compatible between versions 0.8.0, 0.9.0, and
0.10.0.

However, we're also aware that if a change makes it particularly hard to
maintain backwards compatibility, we can instead invest time in coaching our
users through the upgrade process.

## Documentation

Documentation is the first thing users look for when using a new product. If
our documentation isn't good, users may get discouraged and decide that using
Kelda isn't worth the effort.

See [Writing Docs](./writing-docs.md) for information on writing documentation.
