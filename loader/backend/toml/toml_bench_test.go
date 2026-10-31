// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package toml_test

import (
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/altessa-s/go-nomos/loader/backend/toml"
)

func BenchmarkBackend_Decode(b *testing.B) {
	backend := &toml.Backend{}
	input := `key = "value"
name = "test"
count = 100

[section]
num = 42
enabled = true

[nested.deep]
value = "nested"
items = [1, 2, 3, 4, 5]

[[array]]
id = 1
name = "first"

[[array]]
id = 2
name = "second"
`

	type Config struct {
		Key     string `toml:"key"`
		Name    string `toml:"name"`
		Count   int    `toml:"count"`
		Section struct {
			Num     int  `toml:"num"`
			Enabled bool `toml:"enabled"`
		} `toml:"section"`
		Nested struct {
			Deep struct {
				Value string `toml:"value"`
				Items []int  `toml:"items"`
			} `toml:"deep"`
		} `toml:"nested"`
		Array []struct {
			ID   int    `toml:"id"`
			Name string `toml:"name"`
		} `toml:"array"`
	}

	b.ResetTimer()
	for b.Loop() {
		var cfg Config
		reader := strings.NewReader(input)
		if err := backend.Decode(reader, &cfg); err != nil {
			b.Fatalf("Decode failed: %v", err)
		}
	}
}

func BenchmarkBackend_DecodeKeys(b *testing.B) {
	type item struct {
		Name   string `toml:"name"`
		Weight int    `toml:"weight"`
	}
	type config struct {
		Name  string          `toml:"name"`
		Items []item          `toml:"items"`
		ByID  map[string]item `toml:"byID"`
	}
	content := `name = "svc"

[[items]]
name = "a"
weight = 0

[[items]]
name = "b"

[byID.a]
weight = 1
`
	t := reflect.TypeFor[config]()
	backend := &toml.Backend{}

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
		Name    string `toml:"name"`
		Section struct {
			Num     int  `toml:"num"`
			Enabled bool `toml:"enabled"`
		} `toml:"section"`
		Items []struct {
			ID int `toml:"id"`
		} `toml:"items"`
		Meta map[string]any `toml:"meta"`
	}
	content := "name = \"svc\"\n\n[section]\nnum = 42\nenabled = true\n\n[[items]]\nid = 1\n\n[meta.free]\nform = 1\n"
	backend := &toml.Backend{}

	for b.Loop() {
		var cfg config
		if err := backend.DecodeStrict(strings.NewReader(content), &cfg); err != nil {
			b.Fatalf("DecodeStrict failed: %v", err)
		}
	}
}

type largeServer struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

type largeConfig struct {
	Name    string                 `toml:"name"`
	Servers []largeServer          `toml:"servers"`
	ByName  map[string]largeServer `toml:"byName"`
}

// largeDocument returns a document with n array-of-tables elements and n
// map tables.
func largeDocument(n int) string {
	var sb strings.Builder
	sb.WriteString("name = \"svc\"\n")
	for i := range n {
		sb.WriteString("\n[[servers]]\nhost = \"h" + strconv.Itoa(i) + "\"\nport = " + strconv.Itoa(i) + "\n")
	}
	for i := range n {
		sb.WriteString("\n[byName.s" + strconv.Itoa(i) + "]\nhost = \"h\"\nport = 1\n")
	}
	return sb.String()
}

func BenchmarkBackend_DecodeStrict_Large(b *testing.B) {
	content := largeDocument(1000)
	backend := &toml.Backend{}
	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	for b.Loop() {
		var cfg largeConfig
		if err := backend.DecodeStrict(strings.NewReader(content), &cfg); err != nil {
			b.Fatalf("DecodeStrict failed: %v", err)
		}
	}
}

func BenchmarkBackend_Decode_Large(b *testing.B) {
	content := largeDocument(1000)
	backend := &toml.Backend{}
	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	for b.Loop() {
		var cfg largeConfig
		if err := backend.Decode(strings.NewReader(content), &cfg); err != nil {
			b.Fatalf("Decode failed: %v", err)
		}
	}
}

func BenchmarkBackend_Encode(b *testing.B) {
	backend := &toml.Backend{}
	var cfg largeConfig
	if err := backend.Decode(strings.NewReader(largeDocument(1000)), &cfg); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := backend.Encode(io.Discard, cfg); err != nil {
			b.Fatalf("Encode failed: %v", err)
		}
	}
}
