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
