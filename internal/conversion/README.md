# conversion

```go
import "github.com/altessa-s/go-nomos/internal/conversion"
```

Pure configuration transformations and conversion orchestration. File formats and command flags remain in outer adapters.

## Key types

| Type | Responsibility |
|------|----------------|
| `Env` | Flatten trees and render deterministic environment documents. |
| `Parser` | Parse environment assignments with bounded array indices. |
| `Markdown` | Render reference documents. |

## Functions

| Function | Behavior |
|----------|----------|
| `Merge` | Load through an injected reader, then merge in lexical filename order. |

The [CLI adapter](../../cmd/nomos/commands/convert) assembles these operations with concrete backends and file access.

## Serialization contracts

`Env.Render` quotes strings with ambiguous scalar types or document delimiters; `FlattenConfig` continues to return raw values. `Parser` decodes
quoted strings without type inference, recognizes escaped double quotes and literal single quotes, and respects quoted commas in arrays. Commented
assignment parsing ignores section headings. Malformed arrays and arrays exceeding 64 nesting levels remain literal strings.

`Env` treats every nested object as a section and converts each key to `SCREAMING_SNAKE_CASE` the way the loader names struct levels
(`fileStore.dir` becomes `FILE_STORE__DIR`). Without a schema, map entries cannot be told apart from sections, so map keys are converted the same way.

`Markdown` escapes table cells so values cannot split table rows or columns.

The parser processes assignments in document order. Repeating the same key after trimming surrounding whitespace replaces its value.
Distinct keys that address the same normalized path (for example, `A.B` and `A__B`, or `PORTS__00` and `PORTS__0`) return
`ErrConflictingKey`. Distinct field or section names that render to the same environment name also conflict, even if their children differ.
Assigning both a whole value and a child, or using a field as both a section and an indexed array, returns the same error in either order.

Only a numeric segment immediately after a field name denotes an array index. Indices must be between 0 and 1023 inclusive.
Negative or out-of-range indices, empty field names, and invalid UTF-8 names return `ErrInvalidInput`.
The parser validates the entire key before checking its path for conflicts.

For configuration maps supplied directly, `Markdown.Render` returns `ErrConflictingKey` on a rendered name collision.
`Env.Render` and `Env.FlattenConfig` visit sibling keys in ascending lexical order. When multiple paths produce the same variable,
the last value emitted in that traversal supplies its value.
