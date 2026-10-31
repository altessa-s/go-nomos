// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

func BenchmarkBackend_Decode(b *testing.B) {
	backend := &yaml3.Backend{}
	yamlContent := `
key: value
section:
  num: 42
  str: hello
  bool: true
  nested:
    deep: data
    list:
      - item1
      - item2
      - item3
another:
  field1: value1
  field2: value2
  field3: value3
`

	b.ResetTimer()
	for b.Loop() {
		var result map[string]any
		reader := strings.NewReader(yamlContent)
		if err := backend.Decode(reader, &result); err != nil {
			b.Fatalf("Decode error: %v", err)
		}
	}
}

func BenchmarkBackend_DecodeKeys(b *testing.B) {
	type item struct {
		Name   string `yaml:"name"`
		Weight int    `yaml:"weight"`
	}
	type config struct {
		Name  string          `yaml:"name"`
		Items []item          `yaml:"items"`
		ByID  map[string]item `yaml:"byID"`
	}
	content := `
name: svc
items:
  - {name: a, weight: 0}
  - {name: b}
byID:
  a: {weight: 1}
  b: {name: b}
`
	t := reflect.TypeFor[config]()
	backend := &yaml3.Backend{}

	for b.Loop() {
		root, err := backend.DecodeKeys(strings.NewReader(content))
		if err != nil {
			b.Fatalf("DecodeKeys error: %v", err)
		}
		fs, _ := root.Fields(t)
		_, _ = fs[1].Elems(t.Field(1).Type)
		_, _ = fs[2].Entries(t.Field(2).Type)
	}
}

func BenchmarkBackend_DecodeStrict(b *testing.B) {
	type config struct {
		Name    string `yaml:"name"`
		Section struct {
			Num     int  `yaml:"num"`
			Enabled bool `yaml:"enabled"`
		} `yaml:"section"`
		Items []struct {
			ID int `yaml:"id"`
		} `yaml:"items"`
		Meta map[string]any `yaml:"meta"`
	}
	content := "name: svc\nsection:\n  num: 42\n  enabled: true\nitems:\n  - id: 1\nmeta:\n  free:\n    form: 1\n"
	backend := &yaml3.Backend{}

	for b.Loop() {
		var cfg config
		if err := backend.DecodeStrict(strings.NewReader(content), &cfg); err != nil {
			b.Fatalf("DecodeStrict failed: %v", err)
		}
	}
}

// largeMapping returns a mapping of n distinct scalar keys.
func largeMapping(n int) string {
	var sb strings.Builder
	for i := range n {
		sb.WriteString("key")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(": value")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// aliasedDocument returns a document whose sequence refers to one anchored
// mapping n times.
func aliasedDocument(n int) string {
	var sb strings.Builder
	sb.WriteString("base: &b {a: 1, b: 2, c: 3}\nitems:\n")
	for range n {
		sb.WriteString("  - *b\n")
	}
	return sb.String()
}

// BenchmarkBackend_Decode_LargeMapping measures Decode of a 5,000-key mapping,
// including the readChecked pre-parse that bounds yaml.v3's key comparisons.
func BenchmarkBackend_Decode_LargeMapping(b *testing.B) {
	content := largeMapping(5000)
	be := &yaml3.Backend{}
	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	for b.Loop() {
		var out map[string]string
		if err := be.Decode(strings.NewReader(content), &out); err != nil {
			b.Fatalf("Decode error: %v", err)
		}
	}
}

func BenchmarkBackend_DecodeStrict_LargeMapping(b *testing.B) {
	content := largeMapping(5000)
	be := &yaml3.Backend{}
	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	for b.Loop() {
		var out map[string]string
		if err := be.DecodeStrict(strings.NewReader(content), &out); err != nil {
			b.Fatalf("DecodeStrict error: %v", err)
		}
	}
}

func BenchmarkBackend_CheckDocument(b *testing.B) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "large mapping", content: largeMapping(5000)},
		{name: "aliases", content: aliasedDocument(1000)},
	}
	be := &yaml3.Backend{}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.content)))
			for b.Loop() {
				if err := be.CheckDocument(tc.content); err != nil {
					b.Fatalf("CheckDocument error: %v", err)
				}
			}
		})
	}
}

func BenchmarkBackend_RewriteScalars(b *testing.B) {
	content := largeMapping(1000)
	be := &yaml3.Backend{}
	cases := []struct {
		name    string
		rewrite backend.ScalarRewrite
	}{
		{
			name: "unchanged",
			rewrite: func(backend.Scalar) (string, bool, bool, error) {
				return "", false, false, nil
			},
		},
		{
			name: "every scalar",
			rewrite: func(s backend.Scalar) (string, bool, bool, error) {
				return s.Value + "!", true, true, nil
			},
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := be.RewriteScalars(content, tc.rewrite); err != nil {
					b.Fatalf("RewriteScalars error: %v", err)
				}
			}
		})
	}
}

func BenchmarkBackend_PrepareDocument(b *testing.B) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "comments only", content: strings.Repeat("# a comment line\n", 200)},
		{name: "content", content: largeMapping(200)},
	}
	be := &yaml3.Backend{}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = be.PrepareDocument(tc.content)
			}
		})
	}
}

func BenchmarkBackend_Encode(b *testing.B) {
	var value map[string]string
	be := &yaml3.Backend{}
	if err := be.Decode(strings.NewReader(largeMapping(1000)), &value); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := be.Encode(io.Discard, value); err != nil {
			b.Fatalf("Encode error: %v", err)
		}
	}
}

func BenchmarkBackend_CommentPaths(b *testing.B) {
	var sb strings.Builder
	for i := range 500 {
		sb.WriteString("section" + strconv.Itoa(i) + ":\n  key: v # note\n  other: w\n")
	}
	content := []byte(sb.String())
	be := &yaml3.Backend{}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := be.CommentPaths(content); err != nil {
			b.Fatalf("CommentPaths error: %v", err)
		}
	}
}

func BenchmarkBackend_Uncomment(b *testing.B) {
	content := []byte(strings.Repeat("# heading\n#key: value\n#  - item\n#mask: ****\n", 250))
	be := &yaml3.Backend{}
	b.ReportAllocs()
	for b.Loop() {
		_ = be.Uncomment(content)
	}
}

func BenchmarkBackend_IsFullyCommented(b *testing.B) {
	content := []byte(strings.Repeat("# heading\n#key: value\n\n", 300))
	be := &yaml3.Backend{}
	b.ReportAllocs()
	for b.Loop() {
		_ = be.IsFullyCommented(content)
	}
}
