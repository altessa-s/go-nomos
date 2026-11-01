// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
)

func TestCanceledConversionDoesNotWrite(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, source, target string }{
		{"environment", "env", "yaml"}, {"structured", "yaml", "toml"}, {"flatten", "yaml", "env"}, {"markdown", "yaml", "md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			input := filepath.Join(dir, "input."+tc.source)
			output := filepath.Join(dir, "output."+tc.target)
			content := "name: test\n"
			if tc.source == "env" {
				content = "NAME=test\n"
			}
			require.NoError(t, os.WriteFile(input, []byte(content), 0o600))
			require.NoError(t, os.WriteFile(output, []byte("keep"), 0o600))
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			cmd := convert.New()
			cmd.SetArgs([]string{"--from", input, "--to", output})
			require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
			got, err := os.ReadFile(output)
			require.NoError(t, err)
			require.Equal(t, "keep", string(got))
		})
	}
}
