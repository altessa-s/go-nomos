//go:build unix

// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

// A FIFO in the configuration directory is refused at once instead of
// blocking the load on opening it.
func TestLoad_RejectsFIFO(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "base.yaml"), []byte("name: x\n"), 0o600))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "extra.yaml"), 0o600))

	done := make(chan error, 1)
	go func() {
		_, err := loader.New(&yaml3.Backend{}, loader.WithPath(dir)).LoadContext(t.Context(), &nameConfig{})
		done <- err
	}()
	select {
	case err := <-done:
		require.ErrorIs(t, err, loader.ErrNotRegularFile)
	case <-time.After(5 * time.Second):
		t.Fatal("the load blocked on a FIFO")
	}
}
