// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rootcmd

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/go-atlas-core/runtime/appinfo"
)

// Command wraps a [cobra.Command] with shared pre-run and post-run hooks
// including panic recovery.
type Command struct {
	*cobra.Command
}

// New creates a root command with generated functional options.
func New(use string, opts ...Option) *cobra.Command {
	cfg := newOptions(opts...)
	root := &Command{
		Command: &cobra.Command{
			Use:          use,
			Short:        cfg.short,
			Long:         cfg.long,
			Example:      cfg.example,
			SilenceUsage: true,
			Version:      appinfo.Version,
		},
	}

	root.configure(cfg.subcommands)
	if cfg.out != nil {
		root.SetOut(cfg.out)
	}
	if cfg.err != nil {
		root.SetErr(cfg.err)
	}

	return root.Command
}

// configure sets up the root command with all its subcommands and configurations.
func (c *Command) configure(subcommands []*cobra.Command) {
	// Set standard streams
	// Options may replace the default streams.
	c.SetErr(os.Stderr)
	c.SetOut(os.Stdout)

	// Configure command suggestions
	c.SuggestionsMinimumDistance = 1

	// Add version template
	c.SetVersionTemplate(heredoc.Doc(`
		{{with .Name}}{{printf "%s " .}}{{end}}{{printf "version %s" .Version}}
	`))

	// Set pre-run hook with panic recovery wrapper
	c.PersistentPreRunE = c.preRunWithRecovery

	// Set post-run hook for cleanup
	c.PersistentPostRun = c.postRun

	// Add subcommands
	if len(subcommands) > 0 {
		c.AddCommand(subcommands...)
	}
}

// preRunWithRecovery wraps preRun with panic recovery mechanism.
func (c *Command) preRunWithRecovery(cmd *cobra.Command, args []string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Panic recovered: %v\n", r)
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Stack trace:\n%s\n", debug.Stack())
			err = fmt.Errorf("panic recovered: %v", r)
		}
	}()
	return c.preRun(cmd, args)
}

// preRun performs common initialization before any command execution.
func (c *Command) preRun(_ *cobra.Command, _ []string) error { return nil }

// postRun performs cleanup after command execution.
func (c *Command) postRun(_ *cobra.Command, _ []string) {}

// Run creates and executes a command, recovering panics from the command tree.
func Run(ctx context.Context, use string, args []string, opts ...Option) (err error) {
	cmd := New(use, opts...)
	cmd.SetArgs(args)
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Panic recovered: %v\nStack trace:\n%s\n", r, debug.Stack())
			err = fmt.Errorf("panic recovered: %v", r)
		}
	}()
	return cmd.ExecuteContext(ctx)
}
