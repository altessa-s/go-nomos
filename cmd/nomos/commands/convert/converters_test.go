// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
)

const converterYAML = "server:\n  host: localhost\n  port: 8080\n  tags: [a, b]\nlog:\n  level: info\n"

const converterEnv = `# ==========================================
# Log Configuration
# ==========================================
LOG__LEVEL=info

# ==========================================
# Server Configuration
# ==========================================
SERVER__HOST=localhost
SERVER__PORT=8080
SERVER__TAGS=[a,b]
`

const converterTOML = "[log]\n  level = \"info\"\n\n[server]\n  host = \"localhost\"\n  port = 8080\n  tags = [\"a\", \"b\"]\n"

const converterMarkdown = "# Environment Variables Reference\n\n" +
	"This document describes all available environment variables for configuration.\n\n" +
	"## Table of Contents\n\n" +
	"- [Log Configuration](#log-configuration)\n" +
	"- [Server Configuration](#server-configuration)\n\n" +
	"## Log Configuration\n\n" +
	"| Environment Variable | Type | Default Value |\n" +
	"|---------------------|------|---------------|\n" +
	"| `LOG__LEVEL` | string | `info` |\n\n" +
	"## Server Configuration\n\n" +
	"| Environment Variable | Type | Default Value |\n" +
	"|---------------------|------|---------------|\n" +
	"| `SERVER__HOST` | string | `localhost` |\n" +
	"| `SERVER__PORT` | int | `8080` |\n" +
	"| `SERVER__TAGS` | []string | `[a,b]` |\n\n"

func converterData() map[string]any {
	return map[string]any{
		"server": map[string]any{"host": "localhost", "port": 8080, "tags": []any{"a", "b"}},
		"log":    map[string]any{"level": "info"},
	}
}

func TestConfigToConfigConverter_Convert(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	from := writeFile(t, filepath.Join(dir, "in.yaml"), converterYAML)
	toTOML := filepath.Join(dir, "out.toml")
	require.NoError(t, convert.NewConfigToConfigConverter().Convert(from, toTOML, "yaml", "toml"))
	require.Equal(t, converterTOML, readFile(t, toTOML))

	back := filepath.Join(dir, "back.yaml")
	require.NoError(t, convert.NewConfigToConfigConverter().Convert(toTOML, back, "toml", "yaml"))
	require.Equal(t, "log:\n    level: info\nserver:\n    host: localhost\n    port: 8080\n    tags:\n        - a\n        - b\n", readFile(t, back))
}

func TestConfigToConfigConverter_ConvertData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name    string
		ctx     func(t *testing.T) context.Context
		to      string
		format  string
		want    string
		wantErr error
		anyErr  bool
	}{
		{name: "toml", to: filepath.Join(dir, "a.toml"), format: "toml", want: converterTOML},
		{name: "unsupported format", to: filepath.Join(dir, "a.json"), format: "json", anyErr: true},
		{name: "missing directory", to: filepath.Join(dir, "missing", "a.toml"), format: "toml", wantErr: os.ErrNotExist},
		{
			name: "canceled", to: filepath.Join(dir, "c.toml"), format: "toml", wantErr: context.Canceled,
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conv := convert.NewConfigToConfigConverter()
			var err error
			if tc.ctx != nil {
				err = conv.ConvertDataContext(tc.ctx(t), converterData(), tc.to, tc.format)
			} else {
				err = conv.ConvertData(converterData(), tc.to, tc.format)
			}
			switch {
			case tc.wantErr != nil:
				require.ErrorIs(t, err, tc.wantErr)
			case tc.anyErr:
				require.Error(t, err)
			default:
				require.NoError(t, err)
				require.Equal(t, tc.want, readFile(t, tc.to))
			}
			if tc.wantErr != nil || tc.anyErr {
				_, statErr := os.Stat(tc.to)
				require.ErrorIs(t, statErr, os.ErrNotExist)
			}
		})
	}
}

func TestConfigToEnvConverter_Convert(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name, file, content, format string
	}{
		{"yaml", "in.yaml", converterYAML, "yaml"},
		{"toml", "in.toml", converterTOML, "toml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			from := writeFile(t, filepath.Join(dir, tc.file), tc.content)
			to := filepath.Join(dir, tc.name+".env")
			require.NoError(t, convert.NewConfigToEnvConverter().Convert(from, to, tc.format))
			require.Equal(t, converterEnv, readFile(t, to))
		})
	}
}

func TestConfigToEnvConverter_ConvertErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := convert.NewConfigToEnvConverter().Convert(filepath.Join(dir, "absent.yaml"), filepath.Join(dir, "o.env"), "yaml")
	require.ErrorIs(t, err, os.ErrNotExist)

	bad := writeFile(t, filepath.Join(dir, "bad.yaml"), "a: [unterminated\n")
	require.Error(t, convert.NewConfigToEnvConverter().Convert(bad, filepath.Join(dir, "o.env"), "yaml"))
	_, statErr := os.Stat(filepath.Join(dir, "o.env"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestConfigToEnvConverter_ConvertData(t *testing.T) {
	t.Parallel()
	to := filepath.Join(t.TempDir(), "out.env")
	require.NoError(t, convert.NewConfigToEnvConverter().ConvertData(converterData(), to))
	require.Equal(t, converterEnv, readFile(t, to))
}

func TestConfigToMarkdownConverter_Convert(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name, file, content, format string
	}{
		{"yaml", "in.yaml", converterYAML, "yaml"},
		{"commented yaml", "tpl.yaml", "server:\n  host: localhost\n  port: 8080\n  tags: [a, b]\n#log:\n#  level: info\n", "yaml"},
		{"toml", "in.toml", converterTOML, "toml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			from := writeFile(t, filepath.Join(dir, tc.file), tc.content)
			to := filepath.Join(dir, tc.file+".md")
			require.NoError(t, convert.NewConfigToMarkdownConverter().Convert(from, to, tc.format))
			require.Equal(t, converterMarkdown, readFile(t, to))
		})
	}
}

func TestConfigToMarkdownConverter_ConvertData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	to := filepath.Join(dir, "out.md")
	conv := convert.NewConfigToMarkdownConverter()
	require.NoError(t, conv.ConvertData(converterData(), to))
	require.Equal(t, converterMarkdown, readFile(t, to))

	require.ErrorIs(t, conv.ConvertData(converterData(), filepath.Join(dir, "missing", "out.md")), os.ErrNotExist)

	err := conv.Convert(filepath.Join(dir, "absent.yaml"), to, "yaml")
	require.ErrorIs(t, err, os.ErrNotExist)
}
