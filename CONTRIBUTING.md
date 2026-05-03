# Contributing

## Development Flow

1. Create a branch for your change.
2. Open a pull request against `main`.
3. Make sure CI passes.
4. Squash merge or otherwise ensure the final commit message on `main` follows the conventions below.

## Commit And PR Title Conventions

This repository uses `release-please` for Go releases. Release detection is driven by conventional commit style messages.

Preferred formats:

```text
feat: add store catalog validation
fix: reject missing near radius
docs: clarify JSON query model
test: add query request coverage
build: add release workflow
chore: tidy repository scaffolding
```

Common types:

- `feat`: user-facing feature, usually triggers a minor release
- `fix`: bug fix, usually triggers a patch release
- `docs`: documentation-only changes
- `test`: test-only changes
- `build`: CI, workflow, or build tooling changes
- `chore`: maintenance work with no user-facing behavior change

For breaking changes, include `!` in the type or a `BREAKING CHANGE:` note in the body.

## Releases

On pushes to `main`, `release-please` may open or update a release PR.

When that release PR is merged:

1. a Git tag is created for the module
2. a GitHub release is created
