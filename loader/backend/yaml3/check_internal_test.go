// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// allowedAliasRatio reproduces yaml.v3's ratio: the maximum up to the low
// range bound, the minimum from the high one, and a linear ramp in between.
func TestAllowedAliasRatio(t *testing.T) {
	t.Parallel()

	mid := int64(aliasRatioRangeLow+aliasRatioRangeHigh) / 2
	tests := []struct {
		name    string
		decodes int64
		want    float64
	}{
		{name: "zero", decodes: 0, want: maxAliasRatio},
		{name: "low bound", decodes: aliasRatioRangeLow, want: maxAliasRatio},
		{name: "midpoint", decodes: mid, want: (maxAliasRatio + minAliasRatio) / 2},
		{name: "high bound", decodes: aliasRatioRangeHigh, want: minAliasRatio},
		{name: "beyond high bound", decodes: expansionCap, want: minAliasRatio},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.InDelta(t, tc.want, allowedAliasRatio(tc.decodes), 1e-9)
		})
	}

	// The ramp decreases strictly between the bounds.
	prev := allowedAliasRatio(aliasRatioRangeLow)
	for d := int64(aliasRatioRangeLow + 1); d < aliasRatioRangeHigh; d += 360_000 {
		cur := allowedAliasRatio(d)
		require.Less(t, cur, prev)
		prev = cur
	}
}

func TestSameDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	require.NoError(t, sameDir(root, dir))
	require.Error(t, sameDir(root, t.TempDir()), "another directory is not the held one")
	require.ErrorIs(t, sameDir(root, filepath.Join(dir, "missing")), os.ErrNotExist)

	closed, err := os.OpenRoot(dir)
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	require.Error(t, sameDir(closed, dir), "a closed root cannot be checked")
}
