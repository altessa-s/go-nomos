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
