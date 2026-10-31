// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package commands_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands"
)

func TestRunContextPreservesCancellation(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "input.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: test\n"), 0o600))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := commands.RunContext(ctx, []string{"convert", "--from", path, "--to", filepath.Join(t.TempDir(), "out.env")})
	require.ErrorIs(t, err, context.Canceled)
}

func TestNew(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "version", args: []string{"--version"}, want: []string{"nomos version "}},
		{name: "help", args: []string{"--help"}, want: []string{"nomos is a CLI utility", "nomos convert --from config.yaml --to .env", "convert"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := commands.New()
			require.Equal(t, "nomos", cmd.Name())
			require.Equal(t, "Configuration file converter", cmd.Short)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tc.args)
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			for _, w := range tc.want {
				require.Contains(t, out.String(), w)
			}
		})
	}

	sub, _, err := commands.New().Find([]string{"convert"})
	require.NoError(t, err)
	require.Equal(t, "convert", sub.Name())
}

func TestRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	from := filepath.Join(dir, "input.yaml")
	require.NoError(t, os.WriteFile(from, []byte("server:\n  port: 8080\n"), 0o600))
	to := filepath.Join(dir, "out.toml")

	require.NoError(t, commands.Run([]string{"convert", "--from", from, "--to", to}))
	got, err := os.ReadFile(to)
	require.NoError(t, err)
	require.Equal(t, "[server]\n  port = 8080\n", string(got))

	require.Error(t, commands.Run([]string{"convert", "--from", filepath.Join(dir, "absent.yaml"), "--to", to}))
	require.Error(t, commands.Run([]string{"no-such-command"}))
}
