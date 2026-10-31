# yaml3

```go
import "github.com/altessa-s/go-nomos/loader/backend/yaml3"
```

Package `yaml3` provides a YAML backend for the configuration loader. It uses [gopkg.in/yaml.v3](https://github.com/go-yaml/yaml) for parsing and
supports `!include` directives for splitting configuration across multiple files.

## Details

| Property        | Value              |
|-----------------|--------------------|
| Struct tag      | `yaml`             |
| File extensions | `.yaml`, `.yml`    |
| Preprocessor    | Yes (`!include`)   |
| Key decoder     | Yes                |
| Strict decoder  | Yes                |

The zero value of `Backend` is ready to use. This is the default backend when `nil` is passed to `loader.New`.

## !include directive

Split large configs into smaller files using `!include <filename>` on a line of its own, optionally indented beneath a mapping key; inline forms such as
`db: !include db.yaml` are not expanded.

Security constraints:
- Only `.yaml` and `.yml` extensions are allowed
- Included paths must stay within the config root directory (no path traversal)
- Circular includes are detected and rejected
- Maximum nesting depth is 10 levels
- With a positive `maxBytes` (the loader's `WithMaxConfigBytes` cap), each included file and the expanded document are bounded by it; exceeding it
  returns an error wrapping `coreio.ErrReadLimitExceeded`

## Rewriting scalars

`RewriteScalars` (`backend.ScalarRewriter`) offers every scalar node of the document to a callback once — an anchored node once for all its aliases,
mapping keys never — and re-encodes the document when one changed: a literal value as a double-quoted string, any other as a plain scalar whose type
yaml.v3 resolves; a literal value that is not valid UTF-8 becomes a `!!binary` scalar of its bytes, which yaml.v3 decodes into strings, interfaces, byte
slices and text unmarshalers. Key nodes implement `backend.ScalarLister`. The loader resolves `$__secret{ns:key}` placeholders with them before
decoding.

`CheckDocument` (`backend.DocumentChecker`) parses a document into nodes and rejects an anchor holding an alias to itself and excessive aliasing, with
yaml.v3's own expansion ratio, without decoding into Go values, so typed map keys a generic decode cannot represent pass. `RewriteScalars` recognizes a
merge key by yaml.v3's rule: `<<` with an empty, non-specific or `!!merge` tag; a quoted or `!!str` `<<` is an ordinary key.

## Encoding

`Backend.Encode(writer, value)` serializes configuration values for format conversion without exposing parser implementation types.

`Uncomment` prepares commented templates, `IsFullyCommented` detects them, and `CommentPaths` returns paths with inline or head comments. Environment
variable naming remains the conversion layer's responsibility.
