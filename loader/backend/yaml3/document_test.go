// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

func TestBackend_PrepareDocument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", content: "", want: "{}\n"},
		{name: "blank lines", content: "\n  \n\t\n", want: "{}\n"},
		{name: "comments only", content: "# a\n  # b\n#c: 1\n", want: "{}\n"},
		{name: "content kept", content: "a: 1\n", want: "a: 1\n"},
		{name: "content after comments kept", content: "# head\na: 1\n", want: "# head\na: 1\n"},
		{name: "document marker is content", content: "---\n", want: "---\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, (&yaml3.Backend{}).PrepareDocument(tc.content))
		})
	}
}

// A prepared empty document decodes into a struct without error.
func TestBackend_PrepareDocument_Decodes(t *testing.T) {
	t.Parallel()

	b := &yaml3.Backend{}
	var cfg struct {
		A int `yaml:"a"`
	}
	require.NoError(t, b.Decode(strings.NewReader(b.PrepareDocument("# nothing\n")), &cfg))
	require.Zero(t, cfg.A)
}

func TestBackend_Encode(t *testing.T) {
	t.Parallel()

	type inner struct {
		Port int `yaml:"port"`
	}
	type config struct {
		Name   string   `yaml:"name"`
		Server inner    `yaml:"server"`
		Tags   []string `yaml:"tags"`
	}
	in := config{Name: "svc", Server: inner{Port: 8080}, Tags: []string{"a", "b"}}

	b := &yaml3.Backend{}
	var buf bytes.Buffer
	require.NoError(t, b.Encode(&buf, in))
	require.Contains(t, buf.String(), "server:\n    port: 8080\n", "nested values are indented by four spaces")

	var out config
	require.NoError(t, b.Decode(&buf, &out))
	require.Equal(t, in, out)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestBackend_Encode_Errors(t *testing.T) {
	t.Parallel()

	b := &yaml3.Backend{}
	t.Run("writer error", func(t *testing.T) {
		t.Parallel()
		// yaml.v3 reports the writer's error as text, without wrapping it.
		require.Error(t, b.Encode(failingWriter{}, map[string]int{"a": 1}))
	})
	t.Run("unencodable value", func(t *testing.T) {
		t.Parallel()
		require.Error(t, b.Encode(&bytes.Buffer{}, map[string]any{"c": make(chan int)}))
	})
}
