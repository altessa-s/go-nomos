// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"
)

// ErrEmptyInput reports a directory containing no configuration values.
var ErrEmptyInput = errors.New("no valid config files found in directory")

// Merge loads names concurrently, then merges results in lexical filename order.
// The reader owns file I/O; later files override earlier scalar values.
func Merge(ctx context.Context, names []string, read func(context.Context, string) (map[string]any, error)) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names = slices.Clone(names)
	slices.Sort(names)
	type result struct {
		name string
		data map[string]any
	}
	results, err := concurrency.ProcessCollect(ctx, names, func(ctx context.Context, name string) (result, error) {
		data, err := read(ctx, name)
		return result{name, data}, err
	}, concurrency.WithStopOnError[string]())
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(results, func(a, b result) int { return strings.Compare(a.name, b.name) })
	merged := make(map[string]any)
	for _, r := range results {
		mergeInto(merged, r.data)
	}
	if len(merged) == 0 {
		return nil, ErrEmptyInput
	}
	return merged, nil
}
func mergeInto(dst, src map[string]any) {
	for key, value := range src {
		a, aOK := dst[key].(map[string]any)
		b, bOK := value.(map[string]any)
		if aOK && bOK {
			mergeInto(a, b)
		} else {
			dst[key] = value
		}
	}
}
