# toml

```go
import "github.com/altessa-s/go-nomos/loader/backend/toml"
```

Package `toml` provides a TOML backend for the configuration loader. It uses [github.com/BurntSushi/toml](https://github.com/BurntSushi/toml) for
parsing.

## Details

| Property        | Value              |
|-----------------|--------------------|
| Struct tag      | `toml`             |
| File extensions | `.toml`, `.tml`    |
| Preprocessor    | No                 |
| Key decoder     | Yes                |
| Strict decoder  | Yes                |

The zero value of `Backend` is ready to use.

## Encoding

`Backend.Encode(writer, value)` serializes configuration values for format conversion without exposing parser implementation types.
