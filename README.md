# data-star-sig-gen

Standalone public package for:

1. Signal expression helper DSL (Go -> DataStar JS fragments)
2. Signal generator CLI (`signalgen`) for generating typed signal runtime code

## Module

Current module path in this scaffold:

`github.com/imperial-hex/data-star-sig-gen`

If your public repo uses a different path, update `go.mod` before first tag.

## Packages

- `expr`: expression helper builders
- `runtime`: shared runtime helper functions
- `cmd/signalgen`: generator CLI

## Generator config

The generator supports `signalgen.yml` by default.

Example:

```yaml
input: ./cmd/exp
output: ./cmd/exp/signals_gen.go
```

Equivalent keys also supported:

- `inputDir`
- `outputFile`

CLI flags override config values:

- `-config`
- `-input`
- `-output`

## Run

```bash
go run ./cmd/signalgen -config ./signalgen.yml
```

Or without config:

```bash
go run ./cmd/signalgen -input ./cmd/exp -output ./cmd/exp/signals_gen.go
```

## Versioning and releases

This repository follows a patch-only v0 track:

- `v0.0.1`, `v0.0.2`, ..., `v0.0.N`
- No minor/major bumps while on this policy

Release automation is configured with GitHub Actions:

- `.github/workflows/ci.yml` runs tests on pull requests and pushes to `main`
- `.github/workflows/release.yml` runs on pushes to `main`, computes the next `v0.0.x` tag, pushes it, and creates a GitHub Release

Notes:

- Go module versions come from Git tags, not a version field in `go.mod`
- Untagged commits can still be consumed by pseudo-versions, but tagged releases are the canonical public contract
