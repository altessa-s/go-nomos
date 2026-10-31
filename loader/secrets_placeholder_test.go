// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
	loadersecrets "github.com/altessa-s/go-nomos/loader/secrets"
)

// secretPlaceholderConfig holds values that carry $__secret{namespace:key}
// placeholders.
type secretPlaceholderConfig struct {
	Password string `yaml:"password"`
	DSN      string `yaml:"dsn"`
}

// newSecretManager returns a secret resolver serving values; an unknown
// reference fails with core/errors.ErrNotFound.
func newSecretManager(t testing.TB, values map[string]string) loadersecrets.Resolver {
	t.Helper()
	return loadersecrets.ResolverFunc(func(_ context.Context, ref string) (string, error) {
		v, ok := values[ref]
		if !ok {
			return "", fmt.Errorf("secret %q: %w", ref, coreerrs.ErrNotFound)
		}
		return v, nil
	})
}

// writePlaceholderFile writes a YAML file with placeholders and returns its path.
func writePlaceholderFile(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "password: $__secret{app:db_password}\ndsn: \"mongodb://u:$__secret{app:db_password}@db/x\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// Environment substitution of file text used to read "$__secret" as the unset
// variable __secret and drop it, leaving "{app:db_password}" for the secrets
// expander to miss. The placeholder now reaches the expander intact, in
// non-strict and strict mode.
func TestLoad_FileSecretPlaceholder(t *testing.T) {
	t.Parallel()

	manager := newSecretManager(t, map[string]string{"app:db_password": "s3cret"})
	for _, strict := range []bool{false, true} {
		opts := []loader.Option{
			loader.WithPath(writePlaceholderFile(t)),
			loader.WithEnvPrefix("SECRETPLACEHOLDERFILETEST"),
			loader.WithSecretResolver(manager),
		}
		if strict {
			opts = append(opts, loader.WithStrict())
		}
		cfg := &secretPlaceholderConfig{}
		_, err := loader.New(&yaml3.Backend{}, opts...).Load(cfg)
		require.NoError(t, err, "strict=%v", strict)
		require.Equal(t, "s3cret", cfg.Password, "strict=%v", strict)
		require.Equal(t, "mongodb://u:s3cret@db/x", cfg.DSN, "strict=%v", strict)
	}
}

// An unknown secret fails the load instead of leaving the placeholder or an
// empty value.
func TestLoad_FileSecretPlaceholder_Unknown(t *testing.T) {
	t.Parallel()

	manager := newSecretManager(t, map[string]string{"app:other": "x"})
	_, err := loader.New(&yaml3.Backend{},
		loader.WithPath(writePlaceholderFile(t)),
		loader.WithEnvPrefix("SECRETPLACEHOLDERUNKNOWNTEST"),
		loader.WithSecretResolver(manager),
	).Load(&secretPlaceholderConfig{})
	require.ErrorIs(t, err, coreerrs.ErrNotFound)
}

// Without a secrets manager the placeholder stays as written.
func TestLoad_FileSecretPlaceholder_NoManager(t *testing.T) {
	t.Parallel()

	cfg := &secretPlaceholderConfig{}
	_, err := loader.New(&yaml3.Backend{},
		loader.WithPath(writePlaceholderFile(t)),
		loader.WithEnvPrefix("SECRETPLACEHOLDERNOMANAGERTEST"),
	).Load(cfg)
	require.NoError(t, err)
	require.Equal(t, "$__secret{app:db_password}", cfg.Password)
}

// A placeholder in an environment value, whole or embedded, reaches the
// expander in non-strict and strict mode. It mutates the environment, so it is
// not parallel.
func TestLoad_EnvSecretPlaceholder(t *testing.T) {
	manager := newSecretManager(t, map[string]string{"app:db_password": "s3cret"})
	t.Setenv("SECRETPLACEHOLDERENVTEST_PASSWORD", "$__secret{app:db_password}")
	t.Setenv("SECRETPLACEHOLDERENVTEST_DSN", "mongodb://u:$__secret{app:db_password}@db/x")

	for _, strict := range []bool{false, true} {
		opts := []loader.Option{loader.WithEnvPrefix("SECRETPLACEHOLDERENVTEST"), loader.WithSecretResolver(manager)}
		if strict {
			opts = append(opts, loader.WithStrict())
		}
		cfg := &secretPlaceholderConfig{}
		_, err := loader.New(&yaml3.Backend{}, opts...).Load(cfg)
		require.NoError(t, err, "strict=%v", strict)
		require.Equal(t, "s3cret", cfg.Password, "strict=%v", strict)
		require.Equal(t, "mongodb://u:s3cret@db/x", cfg.DSN, "strict=%v", strict)
	}
}

// secretDefaultConfig takes its value from a default tag holding a placeholder.
type secretDefaultConfig struct {
	Password string `yaml:"password" default:"$__secret{app:db_password}"`
}

// A resolved secret is opaque: a payload that itself looks like a placeholder
// is kept as it is, whether the placeholder came from a file, the environment
// or a default tag. It mutates the environment, so it is not parallel.
func TestLoad_SecretPayloadExpandedOnce(t *testing.T) {
	const payload = "$__secret{app:other}"
	manager := newSecretManager(t, map[string]string{"app:db_password": payload, "app:other": "wrong"})

	fromFile := &secretPlaceholderConfig{}
	_, err := loader.New(&yaml3.Backend{},
		loader.WithPath(writePlaceholderFile(t)),
		loader.WithEnvPrefix("SECRETONCEFILETEST"),
		loader.WithSecretResolver(manager),
	).Load(fromFile)
	require.NoError(t, err)
	require.Equal(t, payload, fromFile.Password, "file")

	t.Setenv("SECRETONCEENVTEST_PASSWORD", "$__secret{app:db_password}")
	fromEnv := &secretPlaceholderConfig{}
	_, err = loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETONCEENVTEST"), loader.WithSecretResolver(manager)).Load(fromEnv)
	require.NoError(t, err)
	require.Equal(t, payload, fromEnv.Password, "environment")

	fromDefault := &secretDefaultConfig{}
	_, err = loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETONCEDEFAULTTEST"), loader.WithSecretResolver(manager)).Load(fromDefault)
	require.NoError(t, err)
	require.Equal(t, payload, fromDefault.Password, "default tag")
}

// Only the placeholder syntax $__secret{ is reserved: other references to a
// variable named __secret ($__secret, ${__secret}, embedded or whole) expand
// as usual, and strict mode still reports it undefined. It mutates the
// environment, so it is not parallel.
func TestLoad_SecretVariableIsNotAPlaceholder(t *testing.T) {
	type cfgT struct {
		Braced string `yaml:"braced"`
		Bare   string `yaml:"bare"`
		Mixed  string `yaml:"mixed"`
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "braced: ${__secret}\nbare: $__secret\nmixed: \"a-${__secret}-$__secret{app:db_password}\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	manager := newSecretManager(t, map[string]string{"app:db_password": "s3cret"})

	// Unset: strict mode fails, non-strict yields empty values.
	_, err := loader.New(&yaml3.Backend{}, loader.WithPath(path), loader.WithEnvPrefix("SECRETVARTEST"),
		loader.WithSecretResolver(manager), loader.WithStrict()).Load(&cfgT{})
	require.ErrorIs(t, err, loader.ErrUndefinedEnvVar)

	t.Setenv("__secret", "env-value")
	t.Setenv("SECRETVARTEST_BARE", "x$__secret")
	for _, strict := range []bool{false, true} {
		opts := []loader.Option{loader.WithPath(path), loader.WithEnvPrefix("SECRETVARTEST"), loader.WithSecretResolver(manager)}
		if strict {
			opts = append(opts, loader.WithStrict())
		}
		cfg := &cfgT{}
		_, err = loader.New(&yaml3.Backend{}, opts...).Load(cfg)
		require.NoError(t, err, "strict=%v", strict)
		require.Equal(t, "env-value", cfg.Braced, "strict=%v", strict)
		require.Equal(t, "xenv-value", cfg.Bare, "environment value, strict=%v", strict)
		require.Equal(t, "a-env-value-s3cret", cfg.Mixed, "strict=%v", strict)
	}
}

// overrideConfig has a top-level and a nested secret.
type overrideConfig struct {
	Password string `yaml:"password"`
	DB       struct {
		Password string `yaml:"password"`
	} `yaml:"db"`
	Hosts []string `yaml:"hosts"`
}

// Every placeholder of a file resolves, whether or not the environment
// overrides its value: a missing file secret fails the load even under an
// override, and a resolvable one is resolved and then replaced by the
// override, a literal or a placeholder of its own (whose payload, which
// looks like a placeholder, is kept). It mutates the environment, so it is
// not parallel.
func TestLoad_FileSecretsResolveUnderEnvOverrides(t *testing.T) {
	write := func(content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}
	manager := newSecretManager(t, map[string]string{"app:db_password": "$__secret{app:other}", "app:other": "wrong", "app:file": "file"})
	load := func(path, prefix string) (*overrideConfig, error) {
		cfg := &overrideConfig{}
		_, err := loader.New(&yaml3.Backend{}, loader.WithPath(path), loader.WithEnvPrefix(prefix), loader.WithSecretResolver(manager)).Load(cfg)
		return cfg, err
	}

	missing := write("password: $__secret{app:missing}\ndb:\n  password: $__secret{app:missing}\nhosts: [\"$__secret{app:missing}\"]\n")
	t.Setenv("SECRETOVERRIDEMISSINGTEST_PASSWORD", "literal")
	t.Setenv("SECRETOVERRIDEMISSINGTEST_DB__PASSWORD", "nested-literal")
	t.Setenv("SECRETOVERRIDEMISSINGTEST_HOSTS", "h1,h2")
	_, err := load(missing, "SECRETOVERRIDEMISSINGTEST")
	require.ErrorIs(t, err, coreerrs.ErrNotFound, "a missing file secret fails even when overridden")

	resolvable := write("password: $__secret{app:file}\ndb:\n  password: $__secret{app:file}\nhosts: [\"$__secret{app:file}\"]\n")
	t.Setenv("SECRETOVERRIDELITERALTEST_PASSWORD", "literal")
	t.Setenv("SECRETOVERRIDELITERALTEST_DB__PASSWORD", "nested-literal")
	t.Setenv("SECRETOVERRIDELITERALTEST_HOSTS", "h1,h2")
	cfg, err := load(resolvable, "SECRETOVERRIDELITERALTEST")
	require.NoError(t, err)
	require.Equal(t, "literal", cfg.Password)
	require.Equal(t, "nested-literal", cfg.DB.Password)
	require.Equal(t, []string{"h1", "h2"}, cfg.Hosts)

	t.Setenv("SECRETOVERRIDESECRETTEST_PASSWORD", "$__secret{app:db_password}")
	t.Setenv("SECRETOVERRIDESECRETTEST_DB__PASSWORD", "$__secret{app:db_password}")
	t.Setenv("SECRETOVERRIDESECRETTEST_HOSTS", "$__secret{app:db_password}")
	cfg, err = load(resolvable, "SECRETOVERRIDESECRETTEST")
	require.NoError(t, err)
	require.Equal(t, "$__secret{app:other}", cfg.Password)
	require.Equal(t, "$__secret{app:other}", cfg.DB.Password)
	require.Equal(t, []string{"$__secret{app:other}"}, cfg.Hosts)
}

// Two fields aliasing one storage, the environment overriding one of them: the
// override resolves once (its payload, which looks like a placeholder, is
// kept) and the alias is not resolved again, whichever field comes first. It
// mutates the environment, so it is not parallel.
func TestLoad_EnvOverrideOfAliasedStorage(t *testing.T) {
	manager := newSecretManager(t, map[string]string{"app:first": "$__secret{app:second}", "app:second": "wrong"})
	const payload = "$__secret{app:second}"

	type ab struct {
		A *string  `yaml:"a"`
		B *string  `yaml:"b"`
		L []string `yaml:"l"`
		M []string `yaml:"m"`
	}
	type ba struct {
		B *string  `yaml:"b"`
		A *string  `yaml:"a"`
		M []string `yaml:"m"`
		L []string `yaml:"l"`
	}
	t.Setenv("SECRETALIASTEST_A", "$__secret{app:first}")
	t.Setenv("SECRETALIASTEST_L__0", "$__secret{app:first}")
	load := func(cfg any) {
		t.Helper()
		_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETALIASTEST"), loader.WithSecretResolver(manager)).Load(cfg)
		require.NoError(t, err)
	}

	s1, list1 := "file", []string{"file"}
	first := &ab{A: &s1, B: &s1, L: list1, M: list1}
	load(first)
	require.Equal(t, payload, *first.A)
	require.Equal(t, payload, *first.B)
	require.Equal(t, payload, first.L[0])
	require.Equal(t, payload, first.M[0])

	s2, list2 := "file", []string{"file"}
	second := &ba{A: &s2, B: &s2, L: list2, M: list2}
	load(second)
	require.Equal(t, payload, *second.A)
	require.Equal(t, payload, *second.B)
	require.Equal(t, payload, second.L[0])
	require.Equal(t, payload, second.M[0])
}

// defaultedItem takes its password from a default tag placeholder.
type defaultedItem struct {
	Name     string `yaml:"name"`
	Password string `yaml:"password" default:"$__secret{app:db_password}"`
}

// Elements the environment creates in a slice or a map get their defaults
// later; those defaults take part in the final expansion: a placeholder
// resolves and a missing secret fails. It mutates the environment, so it is
// not parallel.
func TestLoad_DefaultsBelowEnvCreatedElements(t *testing.T) {
	type cfgT struct {
		Items []defaultedItem          `yaml:"items"`
		ByKey map[string]defaultedItem `yaml:"byKey"`
	}
	t.Setenv("SECRETENVDEFAULTTEST_ITEMS__0__NAME", "a")
	t.Setenv("SECRETENVDEFAULTTEST_BY_KEY__k__NAME", "b")

	resolved := newSecretManager(t, map[string]string{"app:db_password": "s3cret"})
	cfg := &cfgT{}
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETENVDEFAULTTEST"), loader.WithSecretResolver(resolved)).Load(cfg)
	require.NoError(t, err)
	require.Len(t, cfg.Items, 1)
	require.Equal(t, "a", cfg.Items[0].Name)
	require.Equal(t, "s3cret", cfg.Items[0].Password)
	require.Equal(t, "b", cfg.ByKey["k"].Name)
	require.Equal(t, "s3cret", cfg.ByKey["k"].Password)

	missing := newSecretManager(t, map[string]string{"app:other": "x"})
	_, err = loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETENVDEFAULTTEST"), loader.WithSecretResolver(missing)).Load(&cfgT{})
	require.ErrorIs(t, err, coreerrs.ErrNotFound)

	// A whole collection assigned from one variable: its elements are
	// created zero and defaulted afterwards.
	t.Setenv("SECRETENVWHOLETEST_ITEMS", "x")
	whole := &cfgT{}
	_, err = loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETENVWHOLETEST"), loader.WithSecretResolver(resolved)).Load(whole)
	require.NoError(t, err)
	for _, item := range whole.Items {
		require.Equal(t, "s3cret", item.Password)
	}
	_, err = loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETENVWHOLETEST"), loader.WithSecretResolver(missing)).Load(&cfgT{})
	require.ErrorIs(t, err, coreerrs.ErrNotFound)
}

// precedenceConfig has a field whose YAML key differs from the camelCase of
// its Go name, so two variable spellings name it.
type precedenceConfig struct {
	FileStore struct {
		Dir  string `yaml:"dir"`
		Port int    `yaml:"port"`
	} `yaml:"filestore"`
	Timeout time.Duration `yaml:"timeout"`
}

// Only the variable precedence selects is resolved: a placeholder in the
// losing Go-field spelling is never looked up. A placeholder for a parsed
// type (int, duration) is resolved before parsing. It mutates the
// environment, so it is not parallel.
func TestLoad_EnvPrecedenceBeforeSecrets(t *testing.T) {
	manager := newSecretManager(t, map[string]string{"app:port": "8080", "app:timeout": "3s"})
	t.Setenv("SECRETPRECTEST_FILE_STORE__DIR", "$__secret{app:missing}")
	t.Setenv("SECRETPRECTEST_FILESTORE__DIR", "/srv/myapp")
	t.Setenv("SECRETPRECTEST_FILE_STORE__PORT", "$__secret{app:missing}")
	t.Setenv("SECRETPRECTEST_FILESTORE__PORT", "$__secret{app:port}")
	t.Setenv("SECRETPRECTEST_TIMEOUT", "$__secret{app:timeout}")

	cfg := &precedenceConfig{}
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETPRECTEST"), loader.WithSecretResolver(manager)).Load(cfg)
	require.NoError(t, err)
	require.Equal(t, "/srv/myapp", cfg.FileStore.Dir)
	require.Equal(t, 8080, cfg.FileStore.Port)
	require.Equal(t, 3*time.Second, cfg.Timeout)
}

// envNamedMap is a defined map type.
type envNamedMap map[string]string

// An environment entry of a map the configuration also holds under another
// type resolves once: its payload, which looks like a placeholder, is kept.
// It mutates the environment, so it is not parallel.
func TestLoad_EnvEntryOfMapHeldUnderTwoTypes(t *testing.T) {
	manager := newSecretManager(t, map[string]string{"app:first": "$__secret{app:second}", "app:second": "wrong"})
	type cfgT struct {
		M map[string]string `yaml:"m"`
		N envNamedMap       `yaml:"n"`
	}
	t.Setenv("SECRETNAMEDMAPTEST_M__k", "$__secret{app:first}")
	shared := map[string]string{}
	cfg := &cfgT{M: shared, N: envNamedMap(shared)}
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETNAMEDMAPTEST"), loader.WithSecretResolver(manager)).Load(cfg)
	require.NoError(t, err)
	require.Equal(t, "$__secret{app:second}", cfg.M["k"])
	require.Equal(t, "$__secret{app:second}", cfg.N["k"])
}

// interfaceConfig has interface targets: the loader assigns environment and
// default values to them as byte slices.
type interfaceConfig struct {
	Value   any            `yaml:"value"`
	Map     map[string]any `yaml:"map"`
	List    []any          `yaml:"list"`
	Default any            `yaml:"default" default:"$__secret{app:db_password}"`
}

// Placeholders assigned to interfaces, directly and as collection elements,
// resolve; a missing secret fails the load. It mutates the environment, so
// it is not parallel.
func TestLoad_SecretPlaceholdersInInterfaces(t *testing.T) {
	t.Setenv("SECRETIFACETEST_VALUE", "$__secret{app:db_password}")
	t.Setenv("SECRETIFACETEST_MAP__k", "x-$__secret{app:db_password}")
	t.Setenv("SECRETIFACETEST_LIST", "$__secret{app:db_password}")

	cfg := &interfaceConfig{}
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETIFACETEST"),
		loader.WithSecretResolver(newSecretManager(t, map[string]string{"app:db_password": "s3cret"}))).Load(cfg)
	require.NoError(t, err)
	require.Equal(t, []byte("s3cret"), cfg.Value)
	require.Equal(t, []byte("x-s3cret"), cfg.Map["k"])
	require.Equal(t, []any{[]byte("s3cret")}, cfg.List)
	require.Equal(t, []byte("s3cret"), cfg.Default)

	for _, name := range []string{"VALUE", "MAP__k", "LIST"} {
		t.Run(name, func(t *testing.T) {
			prefix := "SECRETIFACEMISSING" + strings.ToUpper(strings.ReplaceAll(name, "_", "")) + "TEST"
			t.Setenv(prefix+"_"+name, "$__secret{app:missing}")
			missing := newSecretManager(t, map[string]string{"app:db_password": "s3cret"})
			_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix(prefix), loader.WithSecretResolver(missing)).Load(&interfaceConfig{})
			require.ErrorIs(t, err, coreerrs.ErrNotFound)
		})
	}
}

// recursive is a type whose element type is itself.
type recursive []recursive

// A recursive field type does not hang a load with a secrets manager, with
// an empty override or a placeholder. It mutates the environment, so it is
// not parallel.
func TestLoad_RecursiveTypeWithSecrets(t *testing.T) {
	type cfgT struct {
		R recursive `yaml:"r"`
		S string    `yaml:"s"`
	}
	manager := newSecretManager(t, map[string]string{"app:db_password": "s3cret"})
	t.Setenv("SECRETRECURSIVETEST_R", "")
	t.Setenv("SECRETRECURSIVETEST_S", "$__secret{app:db_password}")
	cfg := &cfgT{}
	_, err := loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETRECURSIVETEST"), loader.WithSecretResolver(manager)).Load(cfg)
	require.NoError(t, err)
	require.Equal(t, "s3cret", cfg.S)

	// A value for the recursive type itself cannot be parsed; it fails
	// instead of recursing forever.
	t.Setenv("SECRETRECURSIVEPHTEST_R", "$__secret{app:db_password}")
	_, err = loader.New(&yaml3.Backend{}, loader.WithEnvPrefix("SECRETRECURSIVEPHTEST"), loader.WithSecretResolver(manager)).Load(&cfgT{})
	require.ErrorIs(t, err, loader.ErrUnsupportedFieldType)
}
