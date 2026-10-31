# backend

```go
import "github.com/altessa-s/go-nomos/loader/backend"
```

Package `backend` defines the `Backend` interface for configuration file format parsers. Implement this interface to add support for new configuration
formats (JSON, INI, etc.).

## Interfaces

| Interface      | Methods                              | Description                              |
|----------------|--------------------------------------|------------------------------------------|
| `Backend`      | `Decode`, `FileExtensions`, `StructTagName` | Main backend contract               |
| `Decoder`      | `Decode(reader, any)`                | Decodes a reader into a struct           |
| `Preprocessor` | `Preprocess(content, currentDir, rootDir, maxBytes)` | Optional content transformation bounded by `maxBytes` |
| `KeyDecoder`   | `DecodeKeys(reader)`                 | Optional: reports which values a document sets |
| `StrictDecoder` | `DecodeStrict(reader, any)`        | Optional: decodes and rejects keys that bind to no struct field |
| `KeyNode`      | `IsNull`, `Fields`, `Entries`, `Elems` | A document value bound to destination types on demand |
| `ScalarRewriter` | `RewriteScalars(content, rewrite)` | Optional: rewrites scalar values before decoding (secret placeholders) |
| `ScalarLister` | `Scalars()`                          | Optional on a `KeyNode`: positions of the scalars at or below it |
| `DocumentChecker` | `CheckDocument(content)`          | Optional: rejects alias cycles and excessive aliasing on the parse tree |

`Preprocessor` is optional. Backends that support directives like `!include` implement it to transform file content before decoding. The loader passes
its `WithMaxConfigBytes` cap as `maxBytes`; a positive value bounds every file the preprocessor reads and the size of the content it returns.

`StrictDecoder` is optional. The loader decodes with it unless `loader.WithAllowUnknownFields()` is set; its error wraps `backend.ErrUnknownField` and
names every key that binds to no field of a destination struct. Map entry keys are never unknown, though struct values in a map are checked; content
under interfaces and types with their own unmarshaler is free-form. Without it the loader decodes with `Decode` and cannot detect unknown keys.

`KeyDecoder` is optional too. The loader uses it to tell an explicit `false`, `0` or `""` from an omitted key, so `default` tags apply only to the
latter. A `KeyNode` binds struct fields, map keys and sequence elements exactly as the backend's `Decode` does; both built-in backends implement it by
decoding into shadow types typed after the destination. Without it the loader falls back to a generic decode matched by tag or field name.

`ScalarRewriter` and `ScalarLister` let the loader resolve `$__secret{ns:key}` placeholders at the source: it binds a file's `KeyNode` tree to the
destination to learn which scalars parse as numbers or bools, and rewrites every scalar holding a placeholder in the document before decoding it. Each
scalar node is offered once as a `Scalar` (position, value, whether it lies inside an anchored node or a `<<` merge), however many aliases use it;
mapping keys are not offered. A backend without them fails a file holding a placeholder when a secrets manager is configured.

## Migrating a `KeyDecoder` (breaking change)

`KeyDecoder.DecodeKeys` used to return `map[string]any` keyed by source spelling. It now returns a `KeyNode`, so map keys, merges and embedding are
bound against the destination type exactly as the backend decodes them. A backend that still implements the old signature no longer satisfies
`KeyDecoder`; without a compile-time assertion the loader silently falls back to the generic decode. Implement the new methods, and assert the interface
so the compiler catches a mismatch:

```go
func (b *Backend) DecodeKeys(r io.Reader) (backend.KeyNode, error) {
	// Parse r and return its root value; an empty document is a null value.
}

var _ backend.KeyDecoder = (*Backend)(nil)
```

A `KeyNode` returns, per call, the children it binds to the given type: `Fields` keyed by field index (an embedded struct the backend flattens maps to a
node holding its fields), `Entries` keyed by the decoded map key, `Elems` in decoded order. Both built-in `KeyNode` implementations are safe for
concurrent use.

## Built-in backends

| Package  | Format | Extensions      | Tag    |
|----------|--------|-----------------|--------|
| `yaml3`  | YAML   | `.yaml`, `.yml` | `yaml` |
| `toml`   | TOML   | `.toml`, `.tml` | `toml` |

## Additional capabilities

| Interface | Contract |
|-----------|----------|
| `DocumentPreparer` | Normalize format-specific empty documents before decoding; absence means pass content unchanged. |
| `SliceReuseDecoder` | Report whether decoding reuses existing slice elements; absence means replacement. |

TOML implements slice reuse. YAML prepares whitespace/comment-only documents as empty mappings. Neither capability changes the required `Backend`
interface, and the loader does not infer either behavior from a format name.
