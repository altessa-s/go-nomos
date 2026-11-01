// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtensionsForFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		format string
		want   []string
	}{
		{formatYAML, []string{".yaml", ".yml"}},
		{formatTOML, []string{".toml"}},
		{formatEnv, []string{".env"}},
		{formatMD, []string{".md"}},
		{"json", nil},
		{"", nil},
	}
	for _, tc := range tests {
		t.Run(tc.format, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, extensionsForFormat(tc.format))
		})
	}
}

func TestConfigToMarkdownConvertDataContext(t *testing.T) {
	t.Parallel()
	data := map[string]any{"db": map[string]any{"host": "h"}}
	const want = "# Environment Variables Reference\n\n" +
		"This document describes all available environment variables for configuration.\n\n" +
		"## Table of Contents\n\n" +
		"- [Db Configuration](#db-configuration)\n\n" +
		"## Db Configuration\n\n" +
		"| Environment Variable | Type | Default Value |\n" +
		"|---------------------|------|---------------|\n" +
		"| `DB__HOST` | string | `h` |\n\n"

	tests := []struct {
		name    string
		cancel  bool
		want    string
		wantErr error
	}{
		{name: "rendered", want: want},
		{name: "canceled", cancel: true, wantErr: context.Canceled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			to := filepath.Join(t.TempDir(), "out.md")
			err := NewConfigToMarkdownConverter().ConvertDataContext(ctx, data, to)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				_, statErr := os.Stat(to)
				require.ErrorIs(t, statErr, os.ErrNotExist)
				return
			}
			require.NoError(t, err)
			got, err := os.ReadFile(to)
			require.NoError(t, err)
			require.Equal(t, tc.want, string(got))
		})
	}
}

func TestOptionsParseComments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		opts      []Option
		wantParse bool
	}{
		{name: "defaults"},
		{name: "parse comments", opts: []Option{WithParseComments()}, wantParse: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := newOptions(tc.opts...)
			require.Equal(t, tc.wantParse, o.parseComments)
			flag := NewWithOptions(tc.opts...).Flags().Lookup("parse-comments")
			require.NotNil(t, flag)
			require.Equal(t, fmt.Sprint(tc.wantParse), flag.DefValue)
		})
	}
}

func TestConfigToEnvConvertDataWithCommentsContextCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	to := filepath.Join(t.TempDir(), "out.env")
	err := NewConfigToEnvConverter().ConvertDataWithCommentsContext(ctx, map[string]any{"a": 1}, nil, to)
	require.ErrorIs(t, err, context.Canceled)
	_, statErr := os.Stat(to)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}
