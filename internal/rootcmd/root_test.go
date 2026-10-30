// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rootcmd_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/internal/rootcmd"
)

func TestRun(t *testing.T) {
	t.Parallel()
	for _, panics := range []bool{false, true} {
		t.Run(fmt.Sprint(panics), func(t *testing.T) {
			t.Parallel()
			var out, errOut bytes.Buffer
			sub := &cobra.Command{Use: "child", RunE: func(cmd *cobra.Command, _ []string) error {
				if panics {
					panic("boom")
				}
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "hello")
				return err
			}}
			err := rootcmd.Run(t.Context(), "testtool", []string{"child"}, rootcmd.WithSubcommands(sub), rootcmd.WithOut(&out), rootcmd.WithErr(&errOut))
			if panics {
				require.Error(t, err)
				require.NotEmpty(t, errOut.String())
			} else {
				require.NoError(t, err)
				require.Equal(t, "hello\n", out.String())
				require.Empty(t, errOut.String())
			}
		})
	}
}
