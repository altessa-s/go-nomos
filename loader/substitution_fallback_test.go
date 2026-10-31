// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/toml"
)

type fallbackConfig struct {
	Name  string `yaml:"name" toml:"name"`
	Other string `yaml:"other" toml:"other" default:"${NOMOS_FALLBACK_UNSET:tag-fallback}"`
}

// The package documents ${VAR:default} substitution: an unset variable takes
// the text after the colon, in files and in default tags, and strict mode
// does not report it as undefined.
func TestLoad_SubstitutionFallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		backend backend.Backend
		file    string
		content string
	}{
		{name: "yaml", file: "a.yaml", content: "name: ${NOMOS_FALLBACK_UNSET:fallback}\n"},
		{name: "toml", backend: &toml.Backend{}, file: "a.toml", content: "name = \"${NOMOS_FALLBACK_UNSET:fallback}\"\n"},
	}
	for _, tc := range tests {
		for _, strict := range []bool{false, true} {
			opts := []loader.Option{loader.WithSkipEnv()}
			if strict {
				opts = append(opts, loader.WithStrict())
			}
			cfg := &fallbackConfig{}
			require.NoError(t, loadFiles(t, tc.backend, cfg, map[string]string{tc.file: tc.content}, opts...), tc.name)
			require.Equal(t, "fallback", cfg.Name, tc.name)
			require.Equal(t, "tag-fallback", cfg.Other, tc.name)
		}
	}
}
