//go:build integration

// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package toml_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/toml"
)

type itTLS struct {
	Cert string `toml:"cert"`
	Key  string `toml:"key"`
}

type itListener struct {
	Name string `toml:"name"`
	Port int    `toml:"port"`
	TLS  *itTLS `toml:"tls"`
}

type itDatabase struct {
	Host    string            `toml:"host"`
	Options map[string]string `toml:"options"`
}

type itConfig struct {
	App       string                `toml:"app"`
	Listeners []itListener          `toml:"listeners"`
	Database  itDatabase            `toml:"database"`
	Replicas  map[string]itDatabase `toml:"replicas"`
	Extra     map[string]any        `toml:"extra"`
}

// writeFiles writes files (path relative to a temp dir) and returns the dir.
func writeFiles(t testing.TB, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	return dir
}

// decodeFile decodes one file of dir into out, strictly when asked.
func decodeFile(t testing.TB, dir, name string, strict bool, out any) error {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, name))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	b := &toml.Backend{}
	if strict {
		return b.DecodeStrict(f, out)
	}
	return b.Decode(f, out)
}

const itBase = `app = "shop"

[[listeners]]
name = "public"
port = 443

[listeners.tls]
cert = "/etc/tls/cert.pem"
key = "/etc/tls/key.pem"

[[listeners]]
name = "admin"
port = 9000

[database]
host = "db.local"

[database.options]
sslmode = "require"

[replicas.eu]
host = "eu.db.local"

[replicas.us]
host = "us.db.local"

[replicas.us.options]
timeout = "5s"

[extra.anything.goes]
deep = [1, 2, 3]
`

// Arrays of tables, sub-tables of array elements, nested map tables and
// free-form tables decode strictly from files.
func TestIntegration_ArraysOfTablesStrict(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{"base.toml": itBase})
	var cfg itConfig
	require.NoError(t, decodeFile(t, dir, "base.toml", true, &cfg))

	require.Equal(t, "shop", cfg.App)
	require.Equal(t, []itListener{
		{Name: "public", Port: 443, TLS: &itTLS{Cert: "/etc/tls/cert.pem", Key: "/etc/tls/key.pem"}},
		{Name: "admin", Port: 9000},
	}, cfg.Listeners)
	require.Equal(t, itDatabase{Host: "db.local", Options: map[string]string{"sslmode": "require"}}, cfg.Database)
	require.Equal(t, map[string]itDatabase{
		"eu": {Host: "eu.db.local"},
		"us": {Host: "us.db.local", Options: map[string]string{"timeout": "5s"}},
	}, cfg.Replicas)
	require.Contains(t, cfg.Extra, "anything")
}

// Layering an override file over the base reuses slice elements, as
// ReusesSliceElements reports.
func TestIntegration_LayeredFiles(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{
		"base.toml":     itBase,
		"override.toml": "[[listeners]]\nport = 8443\n\n[database]\nhost = \"db.prod\"\n",
	})
	var cfg itConfig
	require.NoError(t, decodeFile(t, dir, "base.toml", true, &cfg))
	require.NoError(t, decodeFile(t, dir, "override.toml", true, &cfg))

	require.True(t, (&toml.Backend{}).ReusesSliceElements())
	require.Len(t, cfg.Listeners, 1, "the override's array replaces the length")
	require.Equal(t, "public", cfg.Listeners[0].Name, "the reused element keeps fields the override does not set")
	require.Equal(t, 8443, cfg.Listeners[0].Port)
	require.Equal(t, "db.prod", cfg.Database.Host)
	require.Equal(t, map[string]string{"sslmode": "require"}, cfg.Database.Options)
}

// Strict mode names unknown keys at every depth once, wrapping
// backend.ErrUnknownField; free-form tables never report.
func TestIntegration_StrictUnknownKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  string
		want bool
	}{
		{name: "top-level typo", doc: "ap = \"x\"\n", want: true},
		{name: "array of tables element", doc: "[[listeners]]\nname = \"a\"\nprot = 1\n", want: true},
		{name: "array element sub-table", doc: "[[listeners]]\n[listeners.tls]\ncrt = \"x\"\n", want: true},
		{name: "map table value", doc: "[replicas.eu]\nhots = \"x\"\n", want: true},
		{name: "unknown table with children", doc: "[nope.a.b]\nc = 1\nd = 2\n", want: true},
		{name: "map of strings", doc: "[database.options]\nanything = \"x\"\n", want: false},
		{name: "free-form table", doc: "[extra.x.y]\nz = 1\n", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := writeFiles(t, map[string]string{"c.toml": tc.doc})
			var cfg itConfig
			err := decodeFile(t, dir, "c.toml", true, &cfg)
			if !tc.want {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, backend.ErrUnknownField)

			var lenient itConfig
			require.NoError(t, decodeFile(t, dir, "c.toml", false, &lenient), "Decode ignores unknown keys")
		})
	}
}

// DecodeKeys over nested tables reports exactly the keys a file sets, so an
// explicit zero is told apart from an omitted key.
func TestIntegration_DecodeKeysNestedTables(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{
		"base.toml":  itBase,
		"zeros.toml": "app = \"\"\n\n[[listeners]]\nport = 0\n\n[replicas.eu]\nhost = \"\"\n",
	})
	cfgType := reflect.TypeFor[itConfig]()

	keysOf := func(name string) map[int]backend.KeyNode {
		t.Helper()
		f, err := os.Open(filepath.Join(dir, name))
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		root, err := (&toml.Backend{}).DecodeKeys(f)
		require.NoError(t, err)
		fs, ok := root.Fields(cfgType)
		require.True(t, ok)
		return fs
	}

	base := keysOf("base.toml")
	require.Len(t, base, 5)
	listeners, ok := base[1].Elems(cfgType.Field(1).Type)
	require.True(t, ok)
	require.Len(t, listeners, 2)
	public, ok := listeners[0].Fields(reflect.TypeFor[itListener]())
	require.True(t, ok)
	require.Len(t, public, 3)
	tls, ok := public[2].Fields(reflect.TypeFor[itTLS]())
	require.True(t, ok)
	require.Len(t, tls, 2)
	admin, ok := listeners[1].Fields(reflect.TypeFor[itListener]())
	require.True(t, ok)
	require.NotContains(t, admin, 2, "admin sets no tls table")

	replicas, ok := base[3].Entries(cfgType.Field(3).Type)
	require.True(t, ok)
	require.Len(t, replicas, 2)
	us, ok := replicas["us"].Fields(reflect.TypeFor[itDatabase]())
	require.True(t, ok)
	opts, ok := us[1].Entries(reflect.TypeFor[map[string]string]())
	require.True(t, ok)
	require.Contains(t, opts, "timeout")

	zeros := keysOf("zeros.toml")
	require.Contains(t, zeros, 0, "an explicit empty string is set")
	require.NotContains(t, zeros, 2, "database is omitted")
	zl, ok := zeros[1].Elems(cfgType.Field(1).Type)
	require.True(t, ok)
	zf, ok := zl[0].Fields(reflect.TypeFor[itListener]())
	require.True(t, ok)
	require.Equal(t, []int{1}, keys(zf), "only the explicit zero port is set")
}

func keys(m map[int]backend.KeyNode) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
