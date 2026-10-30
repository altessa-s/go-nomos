// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package commands_test

import (
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
