# secrets

```go
import "github.com/altessa-s/go-nomos/loader/secrets"
```

Package `secrets` provides secret placeholder expansion for the configuration loader. It replaces `$__secret{namespace:key}` placeholders in struct
fields and environment variables with actual secret values retrieved from a `Resolver`; the package depends on no secret store.

## Syntax

Placeholders use the format `$__secret{namespace:key}` where **namespace** is a logical grouping (e.g., `myapp`, `database`) and **key** is the specific
secret identifier within the namespace. The resolver is asked for the reference `namespace:key` (provider keys allow `[a-zA-Z0-9_.:-]`).

`Resolver` has one method, `Resolve(ctx, ref) (string, error)`; `ResolverFunc` adapts a function (an environment- or file-based source, a test double).
A secret store is connected by implementing `Resolver` over its client.

## Options

| Option            | Description                                       |
|-------------------|---------------------------------------------------|
| `WithLogger`      | Set `*slog.Logger` for error reporting             |
| `WithFailOnError` | Fail-closed (default `true`) or fail-open on error |

## Error handling

The expander is **fail-closed** by default: if a secret cannot be resolved, `Expand` returns an error. With a nil resolver, content that carries
placeholders fails with `ErrNoResolver` (content without placeholders passes through unchanged); `ExpandString` with a nil resolver does the same. Set
`WithFailOnError(false)` for fail-open behavior where unresolved placeholders become empty strings.

## Convenience functions

| Function        | Description                                           |
|-----------------|-------------------------------------------------------|
| `HasSecrets`    | Check if a string contains `$__secret{}` placeholders |
| `ExpandString`  | One-shot expand with a temporary `Expander`           |

## Single pass

`Expand` replaces every placeholder in one pass: a secret's value is inserted literally and never scanned again, even when it, alone or joined
with the text after it, looks like a placeholder. The configuration loader calls it once per source value — every file scalar before decoding,
whether or not another source overrides it, an environment value or a default tag before parsing — so a resolved value is never expanded twice.
`References` lists the `namespace:key` of each placeholder, for error messages that must not carry a secret's value.

## Thread safety

`Expander` is safe for concurrent use from multiple goroutines.
