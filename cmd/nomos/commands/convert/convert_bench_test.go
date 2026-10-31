// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
)

func BenchmarkFlattenConfig(b *testing.B) {
	data := map[string]any{
		"consumers": map[string]any{
			"user-events":  map[string]any{"workers": 4, "topic": "users"},
			"order-events": map[string]any{"workers": 8, "topic": "orders"},
		},
	}
	conv := convert.NewConfigToEnvConverter()
	b.ReportAllocs()
	for b.Loop() {
		_ = conv.FlattenConfig(data, "")
	}
}

// benchConfig builds a tree of sections × fields for converter benchmarks.
func benchConfig(sections, fields int) map[string]any {
	data := make(map[string]any, sections)
	for s := range sections {
		section := make(map[string]any, fields)
		for f := range fields {
			if f%2 == 0 {
				section[fmt.Sprintf("field%d", f)] = f
			} else {
				section[fmt.Sprintf("field%d", f)] = fmt.Sprintf("value-%d", f)
			}
		}
		section["tags"] = []any{"a", "b"}
		data[fmt.Sprintf("section%d", s)] = section
	}
	return data
}

// benchFile writes data in format to dir and returns the path.
func benchFile(b *testing.B, dir, name, format string, data map[string]any) string {
	b.Helper()
	path := filepath.Join(dir, name)
	if err := convert.NewConfigToConfigConverter().ConvertData(data, path, format); err != nil {
		b.Fatal(err)
	}
	return path
}

func BenchmarkConfigToConfig(b *testing.B) {
	dir := b.TempDir()
	data := benchConfig(20, 20)
	tests := []struct{ name, from, fromFormat, toFormat string }{
		{"yaml_to_toml", benchFile(b, dir, "in.yaml", "yaml", data), "yaml", "toml"},
		{"toml_to_yaml", benchFile(b, dir, "in.toml", "toml", data), "toml", "yaml"},
	}
	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			to := filepath.Join(b.TempDir(), "out."+tc.toFormat)
			conv := convert.NewConfigToConfigConverter()
			b.ReportAllocs()
			for b.Loop() {
				if err := conv.Convert(tc.from, to, tc.fromFormat, tc.toFormat); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkConfigToEnv(b *testing.B) {
	from := benchFile(b, b.TempDir(), "in.yaml", "yaml", benchConfig(20, 20))
	to := filepath.Join(b.TempDir(), "out.env")
	conv := convert.NewConfigToEnvConverter()
	b.ReportAllocs()
	for b.Loop() {
		if err := conv.Convert(from, to, "yaml"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEnvToConfig(b *testing.B) {
	dir := b.TempDir()
	from := filepath.Join(dir, "in.env")
	if err := convert.NewConfigToEnvConverter().ConvertData(benchConfig(20, 20), from); err != nil {
		b.Fatal(err)
	}
	to := filepath.Join(dir, "out.yaml")
	conv := convert.NewEnvToConfigConverter()
	b.ReportAllocs()
	for b.Loop() {
		if err := conv.Convert(from, to, "yaml"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConfigToMarkdown(b *testing.B) {
	from := benchFile(b, b.TempDir(), "in.yaml", "yaml", benchConfig(20, 20))
	to := filepath.Join(b.TempDir(), "out.md")
	conv := convert.NewConfigToMarkdownConverter()
	b.ReportAllocs()
	for b.Loop() {
		if err := conv.Convert(from, to, "yaml"); err != nil {
			b.Fatal(err)
		}
	}
}
