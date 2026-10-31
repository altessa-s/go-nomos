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

func TestNew(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	ran := false
	sub := &cobra.Command{Use: "child", Run: func(*cobra.Command, []string) { ran = true }}
	cmd := rootcmd.New("testtool",
		rootcmd.WithShort("short text"),
		rootcmd.WithLong("long text"),
		rootcmd.WithExample("testtool child"),
		rootcmd.WithSubcommands(sub),
		rootcmd.WithOut(&out),
		rootcmd.WithErr(&errOut),
	)
	require.Equal(t, "testtool", cmd.Use)
	require.Equal(t, "short text", cmd.Short)
	require.Equal(t, "long text", cmd.Long)
	require.Equal(t, "testtool child", cmd.Example)
	require.True(t, cmd.SilenceUsage)

	cmd.SetArgs([]string{"child"})
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.True(t, ran)

	out.Reset()
	cmd.SetArgs([]string{"--version"})
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Regexp(t, `^testtool version \S*\n$`, out.String())
	require.Empty(t, errOut.String())
}
