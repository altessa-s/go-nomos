# devtools

```go
import "github.com/altessa-s/go-nomos/devtools"
```

Pins development tools separately from library dependencies. This package has no runtime behavior; tools are installed through the root Makefile.

## Tools

| Tool | Purpose |
|------|---------|
| golangci-lint, gci | Static checks and import formatting. |
| govulncheck, gosec | Dependency vulnerability and source security checks. |
| benchstat, dupl | Benchmark comparison and duplication checks. |
| go-copyright-checker | Copyright header checks. |
| go-optgen | Generated options, defaults, and value guards. |

`make generate` builds the pinned `go-optgen` into `bin/optgen`; see the [root README](../README.md).
