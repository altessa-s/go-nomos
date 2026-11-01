// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/altessa-s/go-nomos/internal/conversion"
)

func BenchmarkRenderEnv(b *testing.B) {
	data := map[string]any{"server": map[string]any{"port": 8080, "host": "localhost"}}
	for b.Loop() {
		_ = (&conversion.Env{}).Render(data, nil, false)
	}
}

// largeConfig builds a tree of sections × fields mixing scalars, arrays and nested maps.
func largeConfig(sections, fields int) map[string]any {
	data := make(map[string]any, sections)
	for s := range sections {
		section := make(map[string]any, fields+1)
		for f := range fields {
			switch f % 4 {
			case 0:
				section[fmt.Sprintf("field%d", f)] = f
			case 1:
				section[fmt.Sprintf("field%d", f)] = fmt.Sprintf("value-%d", f)
			case 2:
				section[fmt.Sprintf("field%d", f)] = []any{"a", "b", "c"}
			default:
				section[fmt.Sprintf("field%d", f)] = "30s"
			}
		}
		section["nested"] = map[string]any{"enabled": true, "ratio": 0.5}
		data[fmt.Sprintf("section%d", s)] = section
	}
	return data
}

func BenchmarkParse(b *testing.B) {
	doc := (&conversion.Env{}).Render(largeConfig(50, 20), nil, false)
	parser := &conversion.Parser{}
	b.SetBytes(int64(len(doc)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parser.Parse(bytes.NewReader(doc), false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderEnvLarge(b *testing.B) {
	data := largeConfig(50, 20)
	b.ReportAllocs()
	for b.Loop() {
		_ = (&conversion.Env{}).Render(data, nil, false)
	}
}

func BenchmarkRenderMarkdownLarge(b *testing.B) {
	data := largeConfig(50, 20)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := (&conversion.Markdown{}).Render(b.Context(), data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMerge(b *testing.B) {
	const files = 100
	names := make([]string, files)
	trees := make(map[string]map[string]any, files)
	for i := range files {
		name := fmt.Sprintf("%03d.yaml", i)
		names[i] = name
		trees[name] = largeConfig(5, 10)
	}
	// Merge adopts and updates maps returned by read; identical keys keep the shared trees stable across iterations.
	read := func(_ context.Context, name string) (map[string]any, error) { return trees[name], nil }
	b.ReportAllocs()
	for b.Loop() {
		if _, err := conversion.Merge(b.Context(), names, read); err != nil {
			b.Fatal(err)
		}
	}
}
