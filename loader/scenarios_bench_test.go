// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/altessa-s/go-nomos/loader"

	loadersecrets "github.com/altessa-s/go-nomos/loader/secrets"
)

// writeBenchFiles writes the named files into a temporary directory and
// returns it.
func writeBenchFiles(b *testing.B, files map[string]string) string {
	b.Helper()
	dir := b.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	return dir
}

// runLoad loads cfg once to fail fast on a broken setup, then benchmarks it.
func runLoad(b *testing.B, l *loader.Config, newCfg func() any) {
	b.Helper()
	if _, err := l.LoadContext(b.Context(), newCfg()); err != nil {
		b.Fatal(err)
	}
	cfg := newCfg()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = l.LoadContext(b.Context(), cfg)
	}
}

type benchSecretConfig struct {
	User     string       `yaml:"user"`
	Password string       `yaml:"password"`
	Token    string       `yaml:"token" default:"$__secret{app:token}"`
	DB       *benchNested `yaml:"db"`
}

// BenchmarkLoadContext_SecretResolver loads a file holding secret
// placeholders, resolved through a secrets.Resolver, plus one in a default
// tag.
func BenchmarkLoadContext_SecretResolver(b *testing.B) {
	dir := writeBenchFiles(b, map[string]string{
		"config.yaml": "user: $__secret{app:user}\npassword: $__secret{app:password}\ndb:\n  host: $__secret{db:host}\n",
	})
	values := map[string]string{"app:user": "admin", "app:password": "hunter2", "app:token": "t0k3n", "db:host": "db.internal"}
	resolver := loadersecrets.ResolverFunc(func(_ context.Context, ref string) (string, error) {
		return values[ref], nil
	})
	l := loader.New(nil, loader.WithPath(dir), loader.WithSkipEnv(), loader.WithSecretResolver(resolver))
	runLoad(b, l, func() any { return &benchSecretConfig{} })
}

// BenchmarkLoad_Strict loads a file and environment variables in strict mode:
// unknown keys and undefined variables are checked.
func BenchmarkLoad_Strict(b *testing.B) {
	b.Setenv("BENCHSTRICT_LIMIT", "7")
	b.Setenv("BENCHSTRICT_DB__HOST", "db")
	b.Setenv("BENCHSTRICT_HOST", "cache.internal")
	dir := writeBenchFiles(b, map[string]string{
		"config.yaml": "name: svc\ncache:\n  host: ${BENCHSTRICT_HOST}\nitems:\n  - name: a\n  - name: b\n",
	})
	l := loader.New(nil, loader.WithPath(dir), loader.WithEnvPrefix("BENCHSTRICT"), loader.WithStrict())
	runLoad(b, l, func() any { return &benchConfig{} })
}

// BenchmarkLoad_Directory loads and merges a directory of several files.
func BenchmarkLoad_Directory(b *testing.B) {
	files := make(map[string]string)
	for i := range 8 {
		files[fmt.Sprintf("%02d.yaml", i)] = fmt.Sprintf(
			"name: svc-%d\nlimit: %d\ndb:\n  host: db-%d\nitems:\n  - name: item-%d\nbyName:\n  k%d:\n    weight: %d\n",
			i, i, i, i, i, i)
	}
	dir := writeBenchFiles(b, files)
	l := loader.New(nil, loader.WithPath(dir), loader.WithSkipEnv())
	runLoad(b, l, func() any { return &benchConfig{} })
}

// BenchmarkLoad_EnvAtScale loads 100 prefixed variables: plain, nested,
// indexed slice elements and map entries.
func BenchmarkLoad_EnvAtScale(b *testing.B) {
	b.Setenv("BENCHSCALE_NAME", "svc")
	b.Setenv("BENCHSCALE_LIMIT", "9")
	b.Setenv("BENCHSCALE_DB__HOST", "db")
	b.Setenv("BENCHSCALE_DB__PORT", "6543")
	n := 4
	for i := range 32 {
		b.Setenv(fmt.Sprintf("BENCHSCALE_ITEMS__%d__NAME", i), fmt.Sprintf("item-%d", i))
		b.Setenv(fmt.Sprintf("BENCHSCALE_ITEMS__%d__WEIGHT", i), fmt.Sprint(i))
		b.Setenv(fmt.Sprintf("BENCHSCALE_BY_NAME__key%d__WEIGHT", i), fmt.Sprint(i))
		n += 3
	}
	if n != 100 {
		b.Fatalf("set %d variables, want 100", n)
	}
	l := loader.New(nil, loader.WithEnvPrefix("BENCHSCALE"))
	runLoad(b, l, func() any { return &benchConfig{} })
}

// benchWideSection is a section with a default on every field.
type benchWideSection struct {
	Name    string            `yaml:"name" default:"section"`
	Enabled bool              `yaml:"enabled" default:"true"`
	Limit   int               `yaml:"limit" default:"64"`
	Ratio   float64           `yaml:"ratio" default:"0.75"`
	Timeout string            `yaml:"timeout" default:"30s"`
	Hosts   []string          `yaml:"hosts" default:"a,b,c"`
	Labels  map[string]string `yaml:"labels" default:"env:prod,tier:web"`
	Nested  *benchNested      `yaml:"nested"`
}

// benchWideConfig has 16 sections of 9 defaults each.
type benchWideConfig struct {
	S00, S01, S02, S03, S04, S05, S06, S07 benchWideSection
	S08, S09, S10, S11, S12, S13, S14, S15 *benchWideSection
}

// BenchmarkLoad_DefaultsLargeStruct applies the default tags of a large
// struct into a fresh value on every load.
func BenchmarkLoad_DefaultsLargeStruct(b *testing.B) {
	l := loader.New(nil, loader.WithSkipEnv())
	if _, err := l.Load(&benchWideConfig{}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = l.Load(&benchWideConfig{})
	}
}

type benchSubstConfig struct {
	Host    string   `yaml:"host" default:"${BENCHSUBST_HOST}"`
	URL     string   `yaml:"url" default:"https://${BENCHSUBST_HOST}:${BENCHSUBST_PORT}/${BENCHSUBST_PATH}"`
	Port    int      `yaml:"port" default:"${BENCHSUBST_PORT}"`
	Peers   []string `yaml:"peers" default:"${BENCHSUBST_HOST},${BENCHSUBST_PEER}"`
	Missing string   `yaml:"missing" default:"x-${BENCHSUBST_UNSET}"`
}

// BenchmarkLoad_DefaultsSubstitution applies default tags that reference
// environment variables with ${VAR}.
func BenchmarkLoad_DefaultsSubstitution(b *testing.B) {
	b.Setenv("BENCHSUBST_HOST", "example.internal")
	b.Setenv("BENCHSUBST_PORT", "8443")
	b.Setenv("BENCHSUBST_PATH", "api/v1")
	b.Setenv("BENCHSUBST_PEER", "peer.internal")
	l := loader.New(nil, loader.WithSkipEnv())
	if _, err := l.Load(&benchSubstConfig{}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = l.Load(&benchSubstConfig{})
	}
}
