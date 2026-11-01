# go-nomos

[![CI](https://github.com/altessa-s/go-nomos/actions/workflows/ci.yml/badge.svg?branch=develop)](https://github.com/altessa-s/go-nomos/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/altessa-s/go-nomos.svg)](https://pkg.go.dev/github.com/altessa-s/go-nomos)
[![Go Report Card](https://goreportcard.com/badge/github.com/altessa-s/go-nomos)](https://goreportcard.com/report/github.com/altessa-s/go-nomos)
[![Release](https://img.shields.io/github/v/release/altessa-s/go-nomos)](https://github.com/altessa-s/go-nomos/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/altessa-s/go-nomos)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

```go
import "github.com/altessa-s/go-nomos/<package>"
```

Configuration engine for Go services: a multi-source loader with YAML/TOML backends, environment variable mapping, defaults and secret expansion, and
validation helpers for configuration schemas.

Each package can be imported independently without pulling in the rest of the module. The module defines no configuration schema of its own: the loader
binds any struct the caller passes, and `validation` provides the rules schemas use in their `Validate` methods. Secrets come through a `Resolver`
interface, so the loader depends on no secret store.

## Requirements

Go 1.27 or later (`go 1.27.0`, toolchain `go1.27.3`). Development and CI use the Go 1.27 modernizers and race-enabled tests.

## Packages

| Package                                                    | Description                                                                                           |
|------------------------------------------------------------|-------------------------------------------------------------------------------------------------------|
| [loader](./loader)                                         | Multi-source loader: files, environment variables, `default` tags, `${VAR}` and `$__secret{}`         |
| [loader/backend](./loader/backend)                         | `Backend` interface and optional `KeyDecoder`, `StrictDecoder`, `ScalarRewriter` extensions           |
| [loader/backend/yaml3](./loader/backend/yaml3)             | YAML backend on yaml.v3 with `!include` directives — the default                                      |
| [loader/backend/toml](./loader/backend/toml)               | TOML backend on BurntSushi/toml                                                                       |
| [loader/secrets](./loader/secrets)                         | `$__secret{namespace:key}` expansion through a `Resolver`, fail-closed by default                     |
| [validation](./validation)                                 | `validationconfig`: `ValidateStruct`, `PrettyError`, `NestedField`, `ValidateStorage`, duration rules |
| [cmd/nomos](./cmd/nomos)                                   | `nomos` CLI: converts configuration between YAML, TOML, `.env` and Markdown                           |
| [cmd/nomos/commands](./cmd/nomos/commands)                 | Cobra root command wiring and subcommand registration                                                 |
| [cmd/nomos/commands/convert](./cmd/nomos/commands/convert) | `nomos convert` subcommand: file and HTTP adapters around `internal/conversion`                       |

## Usage

```go
type Config struct {
	Port    int           `yaml:"port" default:"8080"`
	Timeout time.Duration `yaml:"timeout" default:"5s"`
}

var cfg Config
_, err := loader.New(nil, // nil selects the YAML backend
	loader.WithPath("config.yaml"),
	loader.WithEnvPrefix("APP"), // APP_PORT, APP_TIMEOUT
).LoadContext(ctx, &cfg)
```

## Design principles

- **Minimal dependencies** — the Go standard library and [go-atlas-core](https://github.com/altessa-s/go-atlas-core), plus package-scoped
  exceptions: `gopkg.in/yaml.v3` (`loader/backend/yaml3`), `BurntSushi/toml` (`loader/backend/toml`), `go-ozzo/ozzo-validation` and
  `altessa-s/ozzo-rules` (`validation`), `spf13/cobra` and `MakeNowJust/heredoc` (`cmd/nomos`, `internal/rootcmd`), and
  [go-atlas-net](https://github.com/altessa-s/go-atlas-net) (`cmd/nomos/commands/convert`: outbound HTTP transport and request interfaces).
- **Strict by default** — file keys that bind to no struct field fail the load, secret lookups fail closed, `!include` stays inside the config
  root, and file reads are capped by `WithMaxConfigBytes`.
- **Secrets stay out of errors** — once a secret was resolved, `Load` withholds error details and names only field paths and variables.
- **Thread-safe by default** — all exported functions and types are safe for concurrent use unless documented otherwise.

## Development

| Command         | Description                                          |
|-----------------|------------------------------------------------------|
| `make test`     | Run all tests                                        |
| `make lint`     | golangci-lint with the repo config (`.golangci.yml`) |
| `make generate` | Install `optgen` and run go generate                 |
| `make bench`    | Run all benchmarks                                   |
| `make fuzz`     | Fuzz every target for `FUZZTIME`                     |

`make generate` builds [go-optgen](https://github.com/altessa-s/go-optgen) at the version pinned in `devtools/go.mod` into `bin/optgen`, then generates
all options. No sibling checkout is required. Commit regenerated files together with their `options.go` sources.

## Architecture verification

`go test ./internal/architecture` enforces format isolation and dependency direction. The CLI assembles backend and HTTP adapters around
`internal/conversion`; loader operations use backend capabilities, with default YAML selection confined to the public constructor.

Each load works on its own state: a failed reload restores the previously published graph in place, preserving aliases, and callers
synchronize access to mutable configuration pointers themselves.
