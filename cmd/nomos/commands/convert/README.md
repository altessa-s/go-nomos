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
| `ConfigToMarkdownConverter` | YAML/TOML to Markdown reference tables                                          |

`New` returns the `convert` `*cobra.Command`.

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

The command composes file adapters with [internal/conversion](../../../../internal/conversion). Core transformations do not import Cobra,
filesystem access, HTTP clients, or concrete format libraries. YAML/TOML imports are confined to their backend packages, including tests.

`commands.RunContext` carries cancellation through conversion. All file converters expose `ConvertContext`; data conversion variants check
cancellation before output. Context-free entry points remain available. Filesystem calls already in progress are not interrupted.

## Options

| Constructor / option | Purpose |
|----------------------|---------|
| `NewWithOptions` | Configure command defaults through generated options. |
| `WithParseComments` | Enable commented assignment parsing by default. |

Environment array indices are bounded to 1024 elements. Parser fuzzing checks deterministic outcomes, including malformed assignments and excessive
indices.

## Output guarantees

Results are written to a temporary file in the destination directory, synced, and renamed over the destination after a final cancellation check.
Existing file permissions are preserved; new files use mode 0644. Symlinks and other non-regular destinations are rejected. Replacement is atomic on
platforms that support atomic file rename; cancellation cannot undo a completed rename. This does not guarantee directory durability after power loss.

Markdown output escapes cell punctuation and line breaks in values so they cannot split table rows or columns.

## Shared primitives

`go-atlas-core/io/files` opens only regular input files, including symlink targets, and refuses FIFOs without blocking.
