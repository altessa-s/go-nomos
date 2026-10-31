// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/internal/conversion"
)

func TestMergeOrderAndErrors(t *testing.T) {
	t.Parallel()
	data, err := conversion.Merge(t.Context(), []string{"b", "a"}, func(_ context.Context, n string) (map[string]any, error) { return map[string]any{"name": n}, nil })
	require.NoError(t, err)
	require.Equal(t, "b", data["name"])
	sentinel := errors.New("read failed")
	_, err = conversion.Merge(t.Context(), []string{"a"}, func(context.Context, string) (map[string]any, error) { return nil, sentinel })
	require.ErrorIs(t, err, sentinel)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = conversion.Merge(ctx, []string{"a"}, func(context.Context, string) (map[string]any, error) {
		t.Error("read after cancellation")
		return nil, nil
	})
	require.ErrorIs(t, err, context.Canceled)
}

// Merging never writes into the maps a reader returned: a later file merges
// into a copy of an earlier file's nested map.
func TestMergeLeavesReaderMapsIntact(t *testing.T) {
	t.Parallel()
	first := map[string]any{"db": map[string]any{"host": "a"}}
	files := map[string]map[string]any{
		"a": first,
		"b": {"db": map[string]any{"port": 1}},
	}
	merged, err := conversion.Merge(t.Context(), []string{"a", "b"}, func(_ context.Context, n string) (map[string]any, error) {
		return files[n], nil
	})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"db": map[string]any{"host": "a", "port": 1}}, merged)
	require.Equal(t, map[string]any{"db": map[string]any{"host": "a"}}, first)
}
