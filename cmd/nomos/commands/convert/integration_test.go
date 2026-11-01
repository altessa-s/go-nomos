//go:build integration

// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands"
	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

// execute runs args through root and returns captured stdout and stderr.
func execute(t testing.TB, root *cobra.Command, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(t.Context())
	return stdout.String(), stderr.String(), err
}

// nomos runs the production command tree.
func nomos(t testing.TB, args ...string) (string, string, error) {
	t.Helper()
	return execute(t, commands.New(), args...)
}

func decodeYAMLFile(t testing.TB, path string) map[string]any {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var out map[string]any
	require.NoError(t, (&yaml3.Backend{}).Decode(f, &out))
	return out
}

func TestIntegration_YAMLTOMLRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := writeFile(t, filepath.Join(dir, "config.yaml"), `server:
  host: example.internal
  port: 8443
  timeout: 15s
  tls:
    enabled: true
    ciphers: [a, b, c]
database:
  dsn: "postgres://user@db:5432/app?sslmode=disable"
  pool: 0.75
  replicas:
    - host: r1
      port: 5433
    - host: r2
      port: 5434
`)
	toml := filepath.Join(dir, "config.toml")
	stdout, stderr, err := nomos(t, "convert", "--from", src, "--to", toml)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Equal(t, "Converting "+src+" (yaml) -> "+toml+" (toml)...\n", stdout)

	back := filepath.Join(dir, "back.yml")
	_, _, err = nomos(t, "convert", "-f", toml, "-t", back)
	require.NoError(t, err)
	require.Equal(t, decodeYAMLFile(t, src), decodeYAMLFile(t, back))

	// Explicit formats on non-standard extensions take the same path.
	custom := filepath.Join(dir, "config.txt")
	_, _, err = nomos(t, "convert", "--from", back, "--to", custom, "--to-format", "toml")
	require.NoError(t, err)
	tomlBytes, err := os.ReadFile(toml)
	require.NoError(t, err)
	customBytes, err := os.ReadFile(custom)
	require.NoError(t, err)
	require.Equal(t, string(tomlBytes), string(customBytes))
}

func TestIntegration_DirectoryToEnvAndBack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configs := filepath.Join(dir, "configs")
	require.NoError(t, os.Mkdir(configs, 0o755))
	writeFile(t, filepath.Join(configs, "10-base.yaml"), "app:\n  name: svc\n  port: 8080\n  debug: false\nlog:\n  level: info\n")
	writeFile(t, filepath.Join(configs, "20-override.yml"), "app:\n  port: 9090\n  hosts: [h1, h2]\nlog:\n  format: json\n")
	writeFile(t, filepath.Join(configs, "notes.txt"), "ignored: true\n")

	env := filepath.Join(dir, "merged.env")
	stdout, _, err := nomos(t, "convert", "--from", configs, "--to", env)
	require.NoError(t, err)
	require.Contains(t, stdout, configs+" (directory) (yaml)")

	const wantEnv = `# ==========================================
# App Configuration
# ==========================================
APP__DEBUG=false
APP__HOSTS=[h1,h2]
APP__NAME=svc
APP__PORT=9090

# ==========================================
# Log Configuration
# ==========================================
LOG__FORMAT=json
LOG__LEVEL=info
`
	require.Equal(t, wantEnv, readFile(t, env))

	back := filepath.Join(dir, "back.yaml")
	_, _, err = nomos(t, "convert", "--from", env, "--to", back)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"app": map[string]any{"debug": false, "hosts": []any{"h1", "h2"}, "name": "svc", "port": 9090},
		"log": map[string]any{"format": "json", "level": "info"},
	}, decodeYAMLFile(t, back))
}

func TestIntegration_Markdown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := writeFile(t, filepath.Join(dir, "config.yaml"), "db:\n  host: localhost\n  port: 5432\n#cache:\n#  ttl: 30s\n")
	md := filepath.Join(dir, "REFERENCE.md")
	_, stderr, err := nomos(t, "convert", "--from", src, "--to", md)
	require.NoError(t, err)
	require.Empty(t, stderr)

	const want = "# Environment Variables Reference\n\n" +
		"This document describes all available environment variables for configuration.\n\n" +
		"## Table of Contents\n\n" +
		"- [Cache Configuration](#cache-configuration)\n" +
		"- [Db Configuration](#db-configuration)\n\n" +
		"## Cache Configuration\n\n" +
		"| Environment Variable | Type | Default Value |\n" +
		"|---------------------|------|---------------|\n" +
		"| `CACHE__TTL` | duration | `30s` |\n\n" +
		"## Db Configuration\n\n" +
		"| Environment Variable | Type | Default Value |\n" +
		"|---------------------|------|---------------|\n" +
		"| `DB__HOST` | string | `localhost` |\n" +
		"| `DB__PORT` | int | `5432` |\n\n"
	require.Equal(t, want, readFile(t, md))
}

func TestIntegration_Errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := writeFile(t, filepath.Join(dir, "good.yaml"), "a: 1\n")
	bad := writeFile(t, filepath.Join(dir, "bad.yaml"), "a: [unterminated\n")
	envDir := filepath.Join(dir, "envs")
	require.NoError(t, os.Mkdir(envDir, 0o755))
	writeFile(t, filepath.Join(envDir, "a.env"), "A=1\n")

	tests := []struct {
		name    string
		args    []string
		wantErr error
	}{
		{name: "missing source", args: []string{"convert", "--from", filepath.Join(dir, "absent.yaml"), "--to", filepath.Join(dir, "o.env")}},
		{name: "malformed yaml", args: []string{"convert", "--from", bad, "--to", filepath.Join(dir, "o.toml")}},
		{name: "undetectable target", args: []string{"convert", "--from", good, "--to", filepath.Join(dir, "o.txt")}},
		{name: "unsupported format", args: []string{"convert", "--from", good, "--to", filepath.Join(dir, "o.env"), "--to-format", "ini"}},
		{name: "env directory", args: []string{"convert", "--from", envDir, "--to", filepath.Join(dir, "o.yaml")}, wantErr: convert.ErrEnvDirectoryInput},
		{name: "missing required flag", args: []string{"convert", "--from", good}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, err := nomos(t, tc.args...)
			require.Error(t, err)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			}
			require.True(t, strings.HasPrefix(stderr, "Error: "), stderr)
		})
	}
	for _, name := range []string{"o.env", "o.toml", "o.txt", "o.yaml"} {
		_, err := os.Stat(filepath.Join(dir, name))
		require.ErrorIs(t, err, os.ErrNotExist, name)
	}
}

func TestIntegration_Binary(t *testing.T) {
	t.Parallel()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "nomos")
	build := exec.CommandContext(t.Context(), goTool, "build", "-o", bin, "github.com/altessa-s/go-nomos/cmd/nomos")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	src := writeFile(t, filepath.Join(dir, "c.yaml"), "server:\n  port: 8080\n")
	dst := filepath.Join(dir, "c.env")
	run := exec.CommandContext(t.Context(), bin, "convert", "--from", src, "--to", dst)
	out, err = run.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, readFile(t, dst), "SERVER__PORT=8080\n")

	var stderr bytes.Buffer
	run = exec.CommandContext(t.Context(), bin, "convert", "--from", filepath.Join(dir, "absent.yaml"), "--to", dst)
	run.Stderr = &stderr
	err = run.Run()
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	require.True(t, ok, "expected exit error, got %v", err)
	require.Equal(t, 1, exitErr.ExitCode())
	require.Contains(t, stderr.String(), "Error: source path does not exist")

	version, err := exec.CommandContext(t.Context(), bin, "--version").Output()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(version), "nomos version "), string(version))
}
