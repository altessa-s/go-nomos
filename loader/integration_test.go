//go:build integration

// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/toml"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"

	loadersecrets "github.com/altessa-s/go-nomos/loader/secrets"
)

// The tests that set process environment variables (t.Setenv) run serially;
// the others run in parallel.

var errITInvalid = errors.New("integration config invalid")

type itDB struct {
	Host     string        `yaml:"host" toml:"host"`
	Port     int           `yaml:"port" toml:"port" default:"5432"`
	Password string        `yaml:"password" toml:"password"`
	Timeout  time.Duration `yaml:"timeout" toml:"timeout" default:"5s"`
}

type itServer struct {
	Name   string            `yaml:"name" toml:"name" default:"svc"`
	Port   int               `yaml:"port" toml:"port"`
	Hosts  []string          `yaml:"hosts" toml:"hosts"`
	Labels map[string]string `yaml:"labels" toml:"labels"`
	Debug  bool              `yaml:"debug" toml:"debug" default:"true"`
}

type itWorker struct {
	Name   string `yaml:"name" toml:"name"`
	Weight int    `yaml:"weight" toml:"weight" default:"1"`
}

type itRoute struct {
	Target  string `yaml:"target" toml:"target"`
	Retries int    `yaml:"retries" toml:"retries" default:"3"`
}

type itConfig struct {
	Mode    string             `yaml:"mode" toml:"mode" default:"prod"`
	Server  itServer           `yaml:"server" toml:"server"`
	DB      *itDB              `yaml:"db" toml:"db"`
	Workers []itWorker         `yaml:"workers" toml:"workers"`
	Routes  map[string]itRoute `yaml:"routes" toml:"routes"`
	APIKey  string             `yaml:"apiKey" toml:"apiKey" default:"$__secret{app:api-key}"`
}

// Validate rejects a server without a port.
func (c *itConfig) Validate() error {
	if c.Server.Port == 0 {
		return errITInvalid
	}
	return nil
}

// Normalize lowercases the server name.
func (c *itConfig) Normalize() {
	c.Server.Name = strings.ToLower(c.Server.Name)
}

// writeTree writes the files, keyed by slash-separated relative path, under
// dir.
func writeTree(t testing.TB, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
}

// itResolver resolves the integration secrets and fails for any other.
func itResolver(t testing.TB) loadersecrets.Resolver {
	t.Helper()
	values := map[string]string{"db:password": "s3cr3t-db", "app:api-key": "s3cr3t-key"}
	return loadersecrets.ResolverFunc(func(_ context.Context, ref string) (string, error) {
		v, ok := values[ref]
		if !ok {
			return "", errors.New("unknown secret")
		}
		return v, nil
	})
}

// itScenario is a configuration directory in one backend's format, with the
// environment it needs.
type itScenario struct {
	name    string
	backend backend.Backend
	files   map[string]string
	env     map[string]string
}

// itScenarios are equivalent configuration directories in each backend's
// format. The YAML one splits the database section into an included file and
// holds a secret placeholder; TOML files cannot hold one
// (ErrSecretsUnsupported), so the TOML scenario takes it from the
// environment.
func itScenarios() []itScenario {
	return []itScenario{
		{
			name:    "yaml",
			backend: &yaml3.Backend{},
			files: map[string]string{
				"00-app.yaml": "mode: staging\nserver:\n  name: MyService\n  port: ${IT_PORT}\n  hosts:\n    - ${IT_HOST}\n" +
					"db:\n  !include parts/db.yaml\n",
				"10-workers.yml": "workers:\n  - name: a\n  - name: b\n    weight: 0\nroutes:\n  api:\n    target: http://api\n",
				"parts/db.yaml":  "host: db.internal\npassword: $__secret{db:password}\n",
				"README.txt":     "not a configuration file\n",
			},
		},
		{
			name:    "toml",
			backend: &toml.Backend{},
			files: map[string]string{
				"00-app.toml": "mode = \"staging\"\n[server]\nname = \"MyService\"\nport = ${IT_PORT}\nhosts = [\"${IT_HOST}\"]\n" +
					"[db]\nhost = \"db.internal\"\n",
				"10-workers.toml": "[[workers]]\nname = \"a\"\n[[workers]]\nname = \"b\"\nweight = 0\n" +
					"[routes.api]\ntarget = \"http://api\"\n",
				"README.txt": "not a configuration file\n",
			},
			env: map[string]string{"IT_DB__PASSWORD": "$__secret{db:password}"},
		},
	}
}

// A configuration directory loads end to end: files merge in name order with
// includes and ${VAR} substitution, prefixed environment variables override
// nested fields, slice elements and map entries, secrets resolve in files
// and default tags, default tags fill what nothing set, an explicit zero
// survives, and Validate and Normalize run.
func TestIntegration_LoadDirectory(t *testing.T) {
	t.Setenv("IT_PORT", "8081")
	t.Setenv("IT_HOST", "edge.internal")
	t.Setenv("IT_DB__PORT", "6543")
	t.Setenv("IT_WORKERS__1__NAME", "b2")
	t.Setenv("IT_SERVER__LABELS__team", "core")
	t.Setenv("IT_ROUTES__admin__TARGET", "http://admin")

	for _, sc := range itScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			for k, v := range sc.env {
				t.Setenv(k, v)
			}
			dir := t.TempDir()
			writeTree(t, dir, sc.files)

			cfg := &itConfig{}
			l := loader.New(sc.backend, loader.WithPath(dir), loader.WithEnvPrefix("IT"), loader.WithSecretResolver(itResolver(t)))
			got, err := l.LoadContext(t.Context(), cfg)
			require.NoError(t, err)
			require.Same(t, cfg, got)
			require.Same(t, cfg, l.Config())

			require.Equal(t, "staging", cfg.Mode)
			require.Equal(t, itServer{
				Name:   "myservice",
				Port:   8081,
				Hosts:  []string{"edge.internal"},
				Labels: map[string]string{"team": "core"},
				Debug:  true,
			}, cfg.Server)
			require.Equal(t, &itDB{Host: "db.internal", Port: 6543, Password: "s3cr3t-db", Timeout: 5 * time.Second}, cfg.DB)
			require.Equal(t, []itWorker{{Name: "a", Weight: 1}, {Name: "b2", Weight: 0}}, cfg.Workers)
			require.Equal(t, map[string]itRoute{
				"api":   {Target: "http://api", Retries: 3},
				"admin": {Target: "http://admin", Retries: 3},
			}, cfg.Routes)
			require.Equal(t, "s3cr3t-key", cfg.APIKey)
		})
	}
}

// A load that fails validation reports ErrValidation with the validator's
// error, and keeps no secret value in the message.
func TestIntegration_ValidationFailure(t *testing.T) {
	t.Setenv("IT_PORT", "0")
	t.Setenv("IT_HOST", "edge.internal")

	for _, sc := range itScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, sc.files)
			_, err := loader.New(sc.backend, loader.WithPath(dir), loader.WithSkipEnv(), loader.WithSecretResolver(itResolver(t))).
				Load(&itConfig{})
			require.ErrorIs(t, err, loader.ErrValidation)
			require.ErrorIs(t, err, errITInvalid)
			require.NotContains(t, err.Error(), "s3cr3t")
		})
	}
}

// Unknown keys are rejected by default and accepted with
// WithAllowUnknownFields; strict mode also rejects an undefined ${VAR}.
func TestIntegration_StrictRejections(t *testing.T) {
	t.Setenv("IT_PORT", "8081")

	tests := []struct {
		name    string
		backend backend.Backend
		unknown map[string]string
		undef   map[string]string
	}{
		{
			name:    "yaml",
			backend: &yaml3.Backend{},
			unknown: map[string]string{"a.yaml": "server:\n  port: 1\n  prot: 2\n"},
			undef:   map[string]string{"a.yaml": "server:\n  port: 1\n  name: ${IT_UNDEFINED_VARIABLE}\n"},
		},
		{
			name:    "toml",
			backend: &toml.Backend{},
			unknown: map[string]string{"a.toml": "[server]\nport = 1\nprot = 2\n"},
			undef:   map[string]string{"a.toml": "[server]\nport = 1\nname = \"${IT_UNDEFINED_VARIABLE}\"\n"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, tc.unknown)
			_, err := loader.New(tc.backend, loader.WithPath(dir), loader.WithSkipEnv()).Load(&itConfig{})
			require.ErrorIs(t, err, loader.ErrDecode)
			require.ErrorIs(t, err, loader.ErrUnknownField)

			cfg := &itConfig{}
			_, err = loader.New(tc.backend, loader.WithPath(dir), loader.WithSkipEnv(), loader.WithAllowUnknownFields()).Load(cfg)
			require.NoError(t, err)
			require.Equal(t, 1, cfg.Server.Port)

			undefDir := t.TempDir()
			writeTree(t, undefDir, tc.undef)
			_, err = loader.New(tc.backend, loader.WithPath(undefDir), loader.WithSkipEnv(), loader.WithStrict()).Load(&itConfig{})
			require.ErrorIs(t, err, loader.ErrDecode)
			require.ErrorIs(t, err, loader.ErrUndefinedEnvVar)

			cfg = &itConfig{}
			_, err = loader.New(tc.backend, loader.WithPath(undefDir), loader.WithSkipEnv()).Load(cfg)
			require.NoError(t, err)
			require.Empty(t, cfg.Server.Name, "an undefined variable is empty outside strict mode")
		})
	}
}

// A TOML file holding a secret placeholder fails the load with
// ErrSecretsUnsupported: its backend cannot rewrite scalars.
func TestIntegration_TOMLFileSecretUnsupported(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.toml": "[db]\npassword = \"$__secret{db:password}\"\n"})
	_, err := loader.New(&toml.Backend{}, loader.WithPath(dir), loader.WithSkipEnv(), loader.WithSecretResolver(itResolver(t))).
		Load(&itConfig{})
	require.ErrorIs(t, err, loader.ErrSecretsUnsupported)
}

// A file of the configured directory that resolves outside it through a
// symlink is rejected with ErrPathOutsideRoot, and so is an include that
// climbs out of the root.
func TestIntegration_RootConfinement(t *testing.T) {
	t.Parallel()

	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"secret.yaml": "server:\n  port: 1\n"})

	t.Run("symlinked file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{"app.yaml": "mode: dev\n"})
		require.NoError(t, os.Symlink(filepath.Join(outside, "secret.yaml"), filepath.Join(dir, "linked.yaml")))
		_, err := loader.New(nil, loader.WithPath(dir), loader.WithSkipEnv()).Load(&itConfig{})
		require.ErrorIs(t, err, loader.ErrPathOutsideRoot)
	})
	t.Run("symlink inside the root", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{"parts/app.yaml": "server:\n  port: 7\n"})
		require.NoError(t, os.Symlink(filepath.Join(dir, "parts", "app.yaml"), filepath.Join(dir, "linked.yaml")))
		cfg := &itConfig{}
		_, err := loader.New(nil, loader.WithPath(dir), loader.WithSkipEnv()).Load(cfg)
		require.NoError(t, err)
		require.Equal(t, 7, cfg.Server.Port)
	})
	t.Run("include escaping the root", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		rel, err := filepath.Rel(dir, filepath.Join(outside, "secret.yaml"))
		require.NoError(t, err)
		writeTree(t, dir, map[string]string{"app.yaml": "!include " + filepath.ToSlash(rel) + "\n"})
		_, err = loader.New(nil, loader.WithPath(dir), loader.WithSkipEnv()).Load(&itConfig{})
		require.ErrorIs(t, err, loader.ErrDecode)
	})
}

// Loading twice reloads the files: the second load sees the changed and
// removed files, Config returns the latest value, and a failed reload keeps
// the value published before.
func TestIntegration_Reload(t *testing.T) {
	t.Parallel()

	for _, sc := range []struct {
		name          string
		backend       backend.Backend
		first, second map[string]string
		broken        string
	}{
		{
			name:    "yaml",
			backend: &yaml3.Backend{},
			first:   map[string]string{"a.yaml": "server:\n  port: 1\n", "b.yaml": "mode: first\n"},
			second:  map[string]string{"a.yaml": "server:\n  port: 2\n"},
			broken:  "a.yaml",
		},
		{
			name:    "toml",
			backend: &toml.Backend{},
			first:   map[string]string{"a.toml": "[server]\nport = 1\n", "b.toml": "mode = \"first\"\n"},
			second:  map[string]string{"a.toml": "[server]\nport = 2\n"},
			broken:  "a.toml",
		},
	} {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, sc.first)
			l := loader.New(sc.backend, loader.WithPath(dir), loader.WithSkipEnv())

			first, err := l.Load(&itConfig{})
			require.NoError(t, err)
			require.Equal(t, "first", first.(*itConfig).Mode)
			require.Equal(t, 1, first.(*itConfig).Server.Port)

			for name := range sc.first {
				require.NoError(t, os.Remove(filepath.Join(dir, name)))
			}
			writeTree(t, dir, sc.second)
			second, err := l.Load(&itConfig{})
			require.NoError(t, err)
			require.Equal(t, "prod", second.(*itConfig).Mode, "a removed file no longer applies")
			require.Equal(t, 2, second.(*itConfig).Server.Port)
			require.Same(t, second, l.Config())

			writeTree(t, dir, map[string]string{sc.broken: "server = [ :\n"})
			_, err = l.Load(&itConfig{})
			require.ErrorIs(t, err, loader.ErrDecode)
			require.Same(t, second, l.Config(), "a failed reload keeps the published value")
			require.Equal(t, 2, l.Config().(*itConfig).Server.Port)
		})
	}
}
