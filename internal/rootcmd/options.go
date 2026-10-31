// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rootcmd

//go:generate optgen generate --type=options
import (
	"io"

	"github.com/spf13/cobra"
)

type options struct {
	short       string
	long        string
	example     string
	subcommands []*cobra.Command
	out         io.Writer
	err         io.Writer
}
