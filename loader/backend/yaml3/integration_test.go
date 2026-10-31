//go:build integration

// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

type itDatabase struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Pool     int    `yaml:"pool"`
}

type itService struct {
	Name    string `yaml:"name"`
	Replica int    `yaml:"replica"`
	Region  string `yaml:"region"`
}

type itConfig struct {
	App      string               `yaml:"app"`
	Database itDatabase           `yaml:"database"`
	Services []itService          `yaml:"services"`
	Limits   map[string]int       `yaml:"limits"`
	Regions  map[string]itService `yaml:"regions"`
}

// writeTree writes files (path relative to dir) and returns dir's resolved
// path, so include paths compare equal under a symlinked temp directory.
func writeTree(t testing.TB, files map[string]string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	return dir
}

// load preprocesses the entry file of dir and decodes it into out.
func load(t testing.TB, dir, entry string, strict bool, out any) error {
	t.Helper()
	b := &yaml3.Backend{}
	raw, err := os.ReadFile(filepath.Join(dir, entry))
	require.NoError(t, err)
	content, err := b.Preprocess(string(raw), filepath.Dir(filepath.Join(dir, entry)), dir, 1<<20)
	if err != nil {
		return err
	}
	if err := b.CheckDocument(content); err != nil {
		return err
	}
	if strict {
		return b.DecodeStrict(strings.NewReader(content), out)
	}
	return b.Decode(strings.NewReader(content), out)
}

var itFixture = map[string]string{
	"config.yaml": `app: shop
database:
  !include db/database.yaml
defaults: &svc
  replica: 2
  region: eu
services:
  - <<: *svc
    name: api
  - <<: *svc
    name: worker
    replica: 5
!include limits.yaml
regions:
  primary: &primary
    name: eu-1
    replica: 3
  backup: *primary
`,
	"db/database.yaml": `host: db.local
port: 5432
!include credentials/creds.yaml
`,
	"db/credentials/creds.yaml": `user: svc
password: hunter2
!include ../../shared/pool.yaml
`,
	"shared/pool.yaml": "pool: 16\n",
	"limits.yaml":      "limits:\n  rps: 100\n  burst: 200\n",
}

// A nested !include chain, anchors, aliases and merge keys decode into the
// configuration as one document.
func TestIntegration_IncludeChainAnchorsAndMerges(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, itFixture)
	var cfg struct {
		itConfig `yaml:",inline"`
		Defaults map[string]any `yaml:"defaults"`
	}
	require.NoError(t, load(t, dir, "config.yaml", true, &cfg))

	require.Equal(t, "shop", cfg.App)
	require.Equal(t, itDatabase{Host: "db.local", Port: 5432, User: "svc", Password: "hunter2", Pool: 16}, cfg.Database)
	require.Equal(t, []itService{
		{Name: "api", Replica: 2, Region: "eu"},
		{Name: "worker", Replica: 5, Region: "eu"},
	}, cfg.Services, "merge keys fill fields, explicit keys win")
	require.Equal(t, map[string]int{"rps": 100, "burst": 200}, cfg.Limits)
	require.Equal(t, cfg.Regions["primary"], cfg.Regions["backup"], "an alias decodes to the anchored value")
}

// DecodeKeys over the expanded document binds merged and aliased values.
func TestIntegration_DecodeKeysOverIncludes(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, itFixture)
	b := &yaml3.Backend{}
	raw, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	require.NoError(t, err)
	content, err := b.Preprocess(string(raw), dir, dir, 0)
	require.NoError(t, err)

	type keysCfg struct {
		itConfig `yaml:",inline"`
		Defaults map[string]any `yaml:"defaults"`
	}
	root, err := b.DecodeKeys(strings.NewReader(content))
	require.NoError(t, err)
	fs, ok := root.Fields(reflect.TypeFor[keysCfg]())
	require.True(t, ok)
	inline, ok := fs[0].Fields(reflect.TypeFor[itConfig]())
	require.True(t, ok)

	cfgType := reflect.TypeFor[itConfig]()
	db, ok := inline[1].Fields(cfgType.Field(1).Type)
	require.True(t, ok)
	require.Len(t, db, 5, "every database key from the include chain is set")

	services, ok := inline[2].Elems(cfgType.Field(2).Type)
	require.True(t, ok)
	require.Len(t, services, 2)
	worker, ok := services[1].Fields(reflect.TypeFor[itService]())
	require.True(t, ok)
	require.Len(t, worker, 3, "merged replica and region plus the explicit name")

	regions, ok := inline[4].Entries(cfgType.Field(4).Type)
	require.True(t, ok)
	backup, ok := regions["backup"].Fields(reflect.TypeFor[itService]())
	require.True(t, ok)
	require.Len(t, backup, 2, "an alias binds the anchored mapping's keys")
}

// Includes cannot leave the config root: neither a relative path, an
// absolute path nor a symlink leading outside it.
func TestIntegration_IncludeRootConfinement(t *testing.T) {
	t.Parallel()

	outside := writeTree(t, map[string]string{"secret.yaml": "password: leaked\n"})
	tests := []struct {
		name  string
		files map[string]string
		link  string // a symlink at root/link.yaml to outside/secret.yaml
	}{
		{name: "relative escape", files: map[string]string{"config.yaml": "!include sub/x.yaml\n", "sub/x.yaml": "!include ../../" + filepath.Base(outside) + "/secret.yaml\n"}},
		{name: "absolute path", files: map[string]string{"config.yaml": "!include " + filepath.Join(outside, "secret.yaml") + "\n"}},
		{name: "symlink", files: map[string]string{"config.yaml": "!include link.yaml\n"}, link: "link.yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := writeTree(t, tc.files)
			if tc.link != "" {
				require.NoError(t, os.Symlink(filepath.Join(outside, "secret.yaml"), filepath.Join(dir, tc.link)))
			}
			var out map[string]any
			err := load(t, dir, "config.yaml", false, &out)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "leaked")
			require.Empty(t, out)
		})
	}
}

// A circular include chain across files fails instead of recursing.
func TestIntegration_CircularIncludeChain(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{
		"config.yaml": "!include a.yaml\n",
		"a.yaml":      "a: 1\n!include b.yaml\n",
		"b.yaml":      "b: 1\n!include a.yaml\n",
	})
	var out map[string]any
	require.Error(t, load(t, dir, "config.yaml", false, &out))
}

// Strict decoding names unknown keys from every included file and wraps both
// backend.ErrUnknownField and the yaml.v3 type error.
func TestIntegration_StrictUnknownFields(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"config.yaml":      "app: shop\ndatabase:\n  !include db/database.yaml\nservices:\n  - name: api\n    replicas: 2\n",
		"db/database.yaml": "host: db.local\nprot: 5432\n",
	}
	dir := writeTree(t, files)

	var lenient itConfig
	require.NoError(t, load(t, dir, "config.yaml", false, &lenient))
	require.Equal(t, "db.local", lenient.Database.Host)

	var strict itConfig
	err := load(t, dir, "config.yaml", true, &strict)
	require.ErrorIs(t, err, backend.ErrUnknownField)
	te, ok := errors.AsType[*yaml.TypeError](err)
	require.True(t, ok)
	require.Len(t, te.Errors, 2, "both the misspelled prot and replicas are reported")
}

// Excessive aliasing spread across includes is rejected before decoding.
func TestIntegration_AliasBombAcrossIncludes(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	sb.WriteString("a: &a [x, x, x, x, x, x, x, x, x, x]\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g"} {
		sb.WriteString(name + ": &" + name + " [")
		for i := range 10 {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("*" + prev)
		}
		sb.WriteString("]\n")
		prev = name
	}
	dir := writeTree(t, map[string]string{
		"config.yaml": "!include bomb.yaml\n",
		"bomb.yaml":   sb.String(),
	})
	var out map[string]any
	require.Error(t, load(t, dir, "config.yaml", false, &out))
}
