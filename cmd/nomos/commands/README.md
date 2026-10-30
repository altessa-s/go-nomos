# commands

```go
import "github.com/altessa-s/go-nomos/cmd/nomos/commands"
```

Package `commands` wires the cobra root command for nomos and registers all subcommands. `New` returns the root `cobra.Command` for embedding in
larger CLI trees. `Run` is a convenience wrapper that sets the given args and executes the command in one call.

## Entry points

| Function | Description                                                                                        |
|----------|----------------------------------------------------------------------------------------------------|
| `New`    | Returns a `*cobra.Command` with the `convert` subcommand already registered                        |
| `RunContext` | Execute the command tree under the caller's cancellation context. |
| `Run`    | Creates a fresh root command, sets args, and executes; returns any error from the command tree      |

## Subpackages

| Package                | Description                                                                              |
|------------------------|------------------------------------------------------------------------------------------|
| [convert](./convert)   | Implements the `convert` subcommand with format-specific converters                      |
