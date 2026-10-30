// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// White-box tests exercise the shared writer used by every output format.
func TestWriteOutput(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		name := "new"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "out.env")
			if existing {
				require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
			}
			require.NoError(t, writeOutput(t.Context(), path, []byte("new")))
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "new", string(got))
			info, err := os.Stat(path)
			require.NoError(t, err)
			if existing && runtime.GOOS != "windows" {
				require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			}
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1)
		})
	}
}

func TestWriteOutputCanceledPreservesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "out.env")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, writeOutput(ctx, path, []byte("new")), context.Canceled)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "old", string(got))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestWriteOutputRejectsSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	path := filepath.Join(dir, "link")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))
	require.NoError(t, os.Symlink(target, path))
	require.ErrorIs(t, writeOutput(t.Context(), path, []byte("new")), os.ErrInvalid)
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "old", string(got))
}

func TestWriteOutputRejectsDirectory(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, writeOutput(t.Context(), t.TempDir(), []byte("new")), os.ErrInvalid)
}

// Cancel after staging starts so the final pre-rename check exercises cleanup.
type cancelBeforeRename struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *cancelBeforeRename) Err() error {
	c.checks++
	if c.checks == 2 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestWriteOutputCancellationAfterStaging(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "out.env")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o600))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stagedContext := &cancelBeforeRename{Context: ctx, cancel: cancel}
	require.ErrorIs(t, writeOutput(stagedContext, path, []byte("replacement")), context.Canceled)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "original", string(got))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "temporary output must be removed")
}
