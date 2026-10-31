# rootcmd

```go
import "github.com/altessa-s/go-nomos/internal/rootcmd"
```

Constructs Cobra roots with version information, streams, and panic recovery. The command name is positional; generated options configure the rest.

## Functions

| Function | Behavior |
|----------|----------|
| `New(use, opts...)` | Construct a command tree. |
| `Run(ctx, use, args, opts...)` | Execute with cancellation and recover command panics. |

## Options

| Option | Purpose |
|--------|---------|
| `WithShort`, `WithLong`, `WithExample` | Help text. |
| `WithSubcommands` | Child commands. |
| `WithOut`, `WithErr` | Output streams; default to standard output and standard error. |
