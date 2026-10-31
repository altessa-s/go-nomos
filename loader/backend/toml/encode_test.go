// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package toml_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/toml"
)

func TestBackend_ReusesSliceElements(t *testing.T) {
	t.Parallel()

	var b backend.Backend = &toml.Backend{}
	reuse, ok := b.(backend.SliceReuseDecoder)
	require.True(t, ok)
	require.True(t, reuse.ReusesSliceElements())

	// The policy it reports is BurntSushi/toml's: a later document decoding
	// into an existing slice keeps fields of the elements it does not set.
	type item struct {
		A int `toml:"a"`
		B int `toml:"b"`
	}
	var cfg struct {
		Items []item `toml:"items"`
	}
	require.NoError(t, b.Decode(strings.NewReader("[[items]]\na = 1\nb = 2\n"), &cfg))
	require.NoError(t, b.Decode(strings.NewReader("[[items]]\na = 3\n"), &cfg))
	require.Equal(t, []item{{A: 3, B: 2}}, cfg.Items)
}

func TestBackend_Encode(t *testing.T) {
	t.Parallel()

	type server struct {
		Host string `toml:"host"`
		Port int    `toml:"port"`
	}
	type config struct {
		Name    string   `toml:"name"`
		Tags    []string `toml:"tags"`
		Server  server   `toml:"server"`
		Servers []server `toml:"servers"`
	}
	in := config{
		Name:    "svc",
		Tags:    []string{"a", "b"},
		Server:  server{Host: "h", Port: 1},
		Servers: []server{{Host: "x", Port: 2}, {Host: "y", Port: 3}},
	}

	b := &toml.Backend{}
	var buf bytes.Buffer
	require.NoError(t, b.Encode(&buf, in))
	require.Contains(t, buf.String(), "[server]")
	require.Contains(t, buf.String(), "[[servers]]")

	var out config
	require.NoError(t, b.DecodeStrict(&buf, &out))
	require.Equal(t, in, out)
}

func TestBackend_Encode_Errors(t *testing.T) {
	t.Parallel()

	require.Error(t, (&toml.Backend{}).Encode(&bytes.Buffer{}, map[string]any{"c": make(chan int)}))
}

func TestBackend_Encode_Nil(t *testing.T) {
	t.Parallel()

	require.Error(t, (&toml.Backend{}).Encode(&bytes.Buffer{}, nil))
}

type hiddenBase struct {
	Port int `toml:"port"`
}

type hiddenConfig struct {
	hiddenBase              // unexported, flattened by the decoder
	Embedded_hiddenBase int `toml:"e"` //nolint:revive,staticcheck // collides with the synthetic shadow name
}

// An unexported embedded struct is flattened under a synthetic exported name
// that avoids the struct's own field names; its node answers only Fields.
func TestBackend_DecodeKeys_UnexportedEmbedding(t *testing.T) {
	t.Parallel()

	root, err := (&toml.Backend{}).DecodeKeys(strings.NewReader("port = 0\ne = 1\n"))
	require.NoError(t, err)
	fs, ok := root.Fields(reflect.TypeFor[hiddenConfig]())
	require.True(t, ok)
	require.Contains(t, fs, 1, "the field named like the synthetic name keeps its key")

	embedded := fs[0]
	require.NotNil(t, embedded)
	require.False(t, embedded.IsNull())
	_, ok = embedded.Entries(reflect.TypeFor[map[string]int]())
	require.False(t, ok, "an embedded struct is not a map")
	_, ok = embedded.Elems(reflect.TypeFor[[]int]())
	require.False(t, ok, "an embedded struct is not an array")
	inner, ok := embedded.Fields(reflect.TypeFor[hiddenBase]())
	require.True(t, ok)
	require.Contains(t, inner, 0, "port binds through the unexported embedding")

	// The decoder agrees.
	var cfg hiddenConfig
	require.NoError(t, (&toml.Backend{}).Decode(strings.NewReader("port = 7\ne = 1\n"), &cfg))
	require.Equal(t, 7, cfg.Port)
	require.Equal(t, 1, cfg.Embedded_hiddenBase)
}
