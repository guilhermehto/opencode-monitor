# Contributing

## Development loop

1. Install dependencies:

```sh
go mod download
```

2. Run local checks before opening a PR:

```sh
make ci
```

3. Run locally while developing:

```sh
make run
```

## Branch naming

Use short descriptive branches with a type prefix:

- `feat/<topic>`
- `fix/<topic>`
- `chore/<topic>`
- `docs/<topic>`

## Commit messages

Use Conventional-style commit subjects, matching existing history:

- `feat(scope): add unreachable footer`
- `fix(ui): keep child rows aligned`
- `chore(build): add goreleaser config`

Keep the subject line imperative and scoped.

## OpenAPI generation

The `internal/oc/generated.go` file is generated from `internal/oc/openapi.json`.

- Capture schema from a running `opencode` instance:

```sh
make capture-schema
```

- Regenerate models:

```sh
make generate
```

`make capture-schema` requires `opencode` on your `PATH`.

## Release flow

Releases are tag-driven.

1. Create and push a semver tag.
2. Run:

```sh
make release
```

Current releases are unsigned. On macOS, users may need to clear quarantine after extraction:

```sh
xattr -d com.apple.quarantine cogitator
```
