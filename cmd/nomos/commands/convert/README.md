# convert

```go
import "github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
```

Package `convert` implements the `nomos convert` subcommand and the converters that transform configuration files between YAML, TOML, `.env`, and
Markdown formats. Directory files are loaded concurrently and merged in lexical filename order.

## Converters

| Type                        | Description                                                                     |
|-----------------------------|---------------------------------------------------------------------------------|
| `ConfigToEnvConverter`      | YAML/TOML to `.env` with optional comment tracking and YAML auto-uncomment      |
| `EnvToConfigConverter`      | `.env` to YAML/TOML with automatic type inference for values                    |
| `ConfigToConfigConverter`   | YAML to TOML or TOML to YAML direct format conversion                          |
| `ConfigToMarkdownConverter` | YAML/TOML to Markdown reference tables with optional Claude AI descriptions     |

`New` returns the `convert` `*cobra.Command`; `ClaudeClient` (`NewClaudeClient(apiKey)`) generates the short field descriptions used by `--use-claude`
and is not safe for concurrent use.

## Naming conventions

Environment variable keys follow the [loader](../../../../loader) conventions. Nested structures are delimited by `__` (double underscore) and
`camelCase` field names become `SCREAMING_SNAKE_CASE` (e.g. `grpc.interceptors.realIP` becomes `GRPC__INTERCEPTORS__REAL_IP`). The converter has no
schema, so every nested object is treated as a section: map keys are converted the same way as field names.

## Conversion checks

Conflicting `.env` paths, such as `APP=value` together with `APP__HOST=localhost`, return `ErrConflictingKey` instead of silently discarding a value.
YAML auto-uncommenting escapes backslashes and quotes when quoting special scalar values, preserving paths and literal escape sequences. `.env`
conversion preserves quoted strings without type inference and decodes double-quoted escape sequences (`\n`, `\r`, `\t`, `\\`, `\"`). Single-quoted
strings are literal; unmatched quotes remain part of the value. Generated strings with whitespace, delimiters, or ambiguous types are quoted.
A directory source in the `.env` format returns `ErrEnvDirectoryInput` before any file is read; directory input is supported for YAML and TOML only.

## Architecture and cancellation

The command composes file and HTTP adapters with [internal/conversion](../../../../internal/conversion). Core transformations do not import Cobra,
filesystem access, HTTP clients, or concrete format libraries. YAML/TOML imports are confined to their backend packages, including tests.

`commands.RunContext` carries cancellation through conversion and description requests. All file converters expose `ConvertContext`; data conversion
variants check cancellation before output. Context-free entry points remain available. Filesystem calls already in progress are not interrupted.

## Options

| Constructor / option | Purpose |
|----------------------|---------|
| `NewWithOptions` | Configure command defaults and provider settings through generated options. |
| `NewClaudeClientWithOptions` | Configure a provider adapter; the required API key remains positional. |
| `WithParseComments` | Enable commented assignment parsing by default. |
| `WithHTTPClient` | Inject an HTTP client, including test transports. |
| `WithRequestTimeout` | Request timeout; default 60 seconds. |
| `WithMaxResponseBytes` | Response size limit; default 4 MiB. |
| `WithEndpoint`, `WithModel` | Provider endpoint and model. |

Provider failures preserve error identity without including response bodies. Environment array indices are bounded to 1024 elements. Parser fuzzing
checks deterministic outcomes, including malformed assignments and excessive indices.

## Output guarantees

Results are written to a temporary file in the destination directory, synced, and renamed over the destination after a final cancellation check.
Existing file permissions are preserved; new files use mode 0644. Symlinks and other non-regular destinations are rejected. Replacement is atomic on
platforms that support atomic file rename; cancellation cannot undo a completed rename. This does not guarantee directory durability after power loss.

Description requests contain field names and inferred types only, never configuration values. Local Markdown output still includes the values and
escapes cell punctuation and line breaks so they cannot split table rows or columns.

## Shared primitives

`go-atlas-core/io/files` opens only regular input files, including symlink targets, and refuses FIFOs without blocking. 
`go-atlas-core/io.ReadAllLimited` enforces response limits without overflowing at `math.MaxInt64`. `go-atlas-net/http/client` supplies the default HTTP
transport with private-address
protection, no automatic retries, and no redirects. `WithHTTPClient` supplies a caller-owned client unchanged, including its transport policies.
The description adapter depends on `httpx.Doer`; tests use `httpx.DoerFunc`. These network dependencies remain outside the conversion core.

`go-optgen` generates positive-value guards for timeouts and response limits. Nonpositive values are ignored, preserving the last valid
option or default.
