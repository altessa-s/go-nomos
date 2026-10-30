//go:build unix

// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert_test

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-atlas-core/io/files"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
)

func TestConvertersRejectFIFO(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		convert func(context.Context, string, string) error
	}{
		{"env", func(ctx context.Context, in, out string) error {
			return convert.NewEnvToConfigConverter().ConvertContext(ctx, in, out, "yaml")
		}},
		{"structured", func(ctx context.Context, in, out string) error {
			return convert.NewConfigToConfigConverter().ConvertContext(ctx, in, out, "yaml", "toml")
		}},
		{"flatten", func(ctx context.Context, in, out string) error {
			return convert.NewConfigToEnvConverter().ConvertContext(ctx, in, out, "yaml")
		}},
		{"markdown", func(ctx context.Context, in, out string) error {
			return convert.NewConfigToMarkdownConverter().ConvertContext(ctx, in, out, "yaml")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			input := filepath.Join(dir, "input")
			require.NoError(t, syscall.Mkfifo(input, 0o600))
			require.ErrorIs(t, tc.convert(t.Context(), input, filepath.Join(dir, "output")), files.ErrNotRegular)
		})
	}
}
