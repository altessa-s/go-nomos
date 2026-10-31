//go:build unix

// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-atlas-core/io/files"

	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

// An !include target that is a FIFO without a writer, directly or through a
// nested include, is refused at once instead of blocking the preprocessor.
func TestBackend_Preprocess_FIFOIncludeRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		main string
		// nested, when set, is a regular include that includes the FIFO.
		nested string
	}{
		{name: "direct", main: "!include extra.yaml\n"},
		{name: "nested", main: "!include middle.yaml\n", nested: "!include extra.yaml\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "extra.yaml"), 0o600))
			if tc.nested != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "middle.yaml"), []byte(tc.nested), 0o600))
			}

			done := make(chan error, 1)
			go func() {
				_, err := (&yaml3.Backend{}).Preprocess(tc.main, dir, dir, 0)
				done <- err
			}()
			select {
			case err := <-done:
				require.ErrorIs(t, err, files.ErrNotRegular)
			case <-time.After(5 * time.Second):
				t.Fatal("the include of a FIFO blocked")
			}
		})
	}
}

// A root directory that is a FIFO fails the first include at once instead of
// blocking the preprocessor.
func TestBackend_Preprocess_FIFORootRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fifo := filepath.Join(dir, "root")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))

	done := make(chan error, 1)
	go func() {
		_, err := (&yaml3.Backend{}).Preprocess("!include extra.yaml\n", fifo, fifo, 0)
		done <- err
	}()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("opening a FIFO root blocked")
	}
}

// An empty root directory is refused: an include is never read from the
// filesystem root.
func TestBackend_Preprocess_EmptyRootRefused(t *testing.T) {
	t.Parallel()
	_, err := (&yaml3.Backend{}).Preprocess("!include extra.yaml\n", ".", "", 0)
	require.Error(t, err)
}
