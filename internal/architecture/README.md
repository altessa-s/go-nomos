# architecture

```go
import "github.com/altessa-s/go-nomos/internal/architecture"
```

Executable architecture checks enforce dependency direction, format isolation, and package documentation. This package has no runtime behavior.

## Checks

| Check | Rule |
|-------|------|
| Format dependencies | YAML and TOML imports belong to their backend packages, including tests. |
| Conversion | Pure transformations cannot import filesystems, HTTP, Cobra, or concrete backends. |
| Loader | Only the public constructor selects a concrete backend. |
| Library direction | Internal packages cannot depend on cmd. |
