// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/backend/yaml3"

	coreio "github.com/altessa-s/go-atlas-core/io"
)

// openRoot opens dir as an os.Root and returns it with dir's resolved path.
func openRoot(t testing.TB, dir string) (*os.Root, string) {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	root, err := os.OpenRoot(resolved)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return root, resolved
}

func TestBackend_PreprocessIn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "leaf.yaml"), []byte("leaf: 1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mid.yaml"), []byte("mid:\n  !include sub/leaf.yaml\n"), 0o600))
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	require.NoError(t, os.WriteFile(outside, []byte("secret: x\n"), 0o600))

	root, resolved := openRoot(t, dir)
	b := &yaml3.Backend{}

	t.Run("nested includes", func(t *testing.T) {
		t.Parallel()
		got, err := b.PreprocessIn("top: 0\n!include mid.yaml\n", resolved, root, resolved, 0)
		require.NoError(t, err)
		require.Equal(t, "top: 0\nmid:\n  leaf: 1\n", got)
	})
	t.Run("include outside the root", func(t *testing.T) {
		t.Parallel()
		_, err := b.PreprocessIn("!include "+outside+"\n", resolved, root, resolved, 0)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret: x")
	})
	t.Run("size bound", func(t *testing.T) {
		t.Parallel()
		_, err := b.PreprocessIn("!include mid.yaml\n", resolved, root, resolved, 4)
		require.ErrorIs(t, err, coreio.ErrReadLimitExceeded)
	})
	t.Run("caller keeps the root open", func(t *testing.T) {
		t.Parallel()
		_, err := b.PreprocessIn("!include mid.yaml\n", resolved, root, resolved, 0)
		require.NoError(t, err)
		_, err = root.Stat("mid.yaml")
		require.NoError(t, err, "PreprocessIn does not close a root it was given")
	})
}
