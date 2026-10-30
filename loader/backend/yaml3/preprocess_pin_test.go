// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProcessIncludes_Pin(t *testing.T) {
	t.Parallel()

	t.Run("an empty include still ends its line", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.yaml"), nil, 0o600))
		root := &includeRoot{dir: dir, given: dir, owned: true}
		t.Cleanup(root.close)

		out, err := processIncludes("a: 1\n!include empty.yaml\nb: 2\n", dir, 0, map[string]bool{}, root, 0)
		require.NoError(t, err)
		require.Equal(t, "a: 1\n\nb: 2\n", out)
	})

	t.Run("a line longer than the scanner buffer fails", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		root := &includeRoot{dir: dir, given: dir, owned: true}
		t.Cleanup(root.close)

		_, err := processIncludes("k: "+strings.Repeat("x", bufio.MaxScanTokenSize)+"\n", dir, 0, map[string]bool{}, root, 0)
		require.ErrorIs(t, err, bufio.ErrTooLong)
	})
}
