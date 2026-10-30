// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package commands

import (
	"context"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
	"github.com/altessa-s/go-nomos/internal/rootcmd"
)

// config returns the root command configuration.
func options() []rootcmd.Option {
	return []rootcmd.Option{
		rootcmd.WithShort("Configuration file converter"),
		rootcmd.WithLong(heredoc.Doc(`
			nomos is a CLI utility for converting between different configuration formats.

			It supports conversion between:
			- YAML and TOML configuration files
			- .env files (environment variables)
			It can also generate Markdown reference documents.

			The converter follows the go-nomos loader naming conventions:
			- Uses __ (double underscore) as section delimiter
			- Converts camelCase field names to SCREAMING_SNAKE_CASE
			- Properly handles nested structures and arrays
		`)),
		rootcmd.WithExample(heredoc.Doc(`
			# Convert YAML to .env
			nomos convert --from config.yaml --to .env

			# Convert .env to YAML
			nomos convert --from .env --to config.yaml

			# Show version information
			nomos --version
		`)),
		rootcmd.WithSubcommands(convert.New()),
	}
}

// New returns the nomos root [cobra.Command] with the convert subcommand
// already registered. The returned command can be embedded in a larger CLI tree
// or executed directly via [cobra.Command.Execute].
func New() *cobra.Command {
	return rootcmd.New("nomos", options()...)
}

// Run creates a fresh root command, sets args, and executes it.
// It returns any error produced by the command tree, including panics
// recovered by [rootcmd.Run].
func Run(args []string) error {
	return RunContext(context.Background(), args)
}

// RunContext executes the command tree with caller cancellation.
func RunContext(ctx context.Context, args []string) error {
	return rootcmd.Run(ctx, "nomos", args, options()...)
}
