// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
	"github.com/altessa-s/go-nomos/loader/backend/toml"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

type parityLeaf struct {
	Enabled bool          `yaml:"enabled" toml:"enabled" default:"true"`
	Limit   int           `yaml:"limit" toml:"limit" default:"5"`
	Timeout time.Duration `yaml:"timeout" toml:"timeout" default:"15m"`
}

type parityConfig struct {
	Name   string                `yaml:"name" toml:"name" default:"svc"`
	Ratio  float64               `yaml:"ratio" toml:"ratio" default:"0.5"`
	Leaf   parityLeaf            `yaml:"leaf" toml:"leaf"`
	Ptr    *parityLeaf           `yaml:"ptr" toml:"ptr"`
	Items  []parityLeaf          `yaml:"items" toml:"items"`
	ByName map[string]parityLeaf `yaml:"byName" toml:"byName"`
}

// The YAML and TOML backends report the keys of a document through their
// own KeyNode implementations. Equivalent documents must bind the same
// values, so an explicit zero beats a default tag and an omitted key keeps it
// in both formats.
func TestLoad_BackendParity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		yaml string
		toml string
	}{
		{
			name: "empty document",
			yaml: "",
			toml: "",
		},
		{
			name: "explicit zeros",
			yaml: "name: \"\"\nratio: 0\nleaf:\n  enabled: false\n  limit: 0\n",
			toml: "name = \"\"\nratio = 0.0\n[leaf]\nenabled = false\nlimit = 0\n",
		},
		{
			name: "pointer section with omitted fields",
			yaml: "ptr:\n  enabled: false\n",
			toml: "[ptr]\nenabled = false\n",
		},
		{
			name: "slice elements with omitted fields",
			yaml: "items:\n  - limit: 0\n  - enabled: false\n",
			toml: "[[items]]\nlimit = 0\n[[items]]\nenabled = false\n",
		},
		{
			name: "map entries with omitted fields",
			yaml: "byName:\n  a:\n    limit: 0\n  b: {}\n",
			toml: "[byName.a]\nlimit = 0\n[byName.b]\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The environment is skipped so only the documents and defaults
			// decide the values.
			fromYAML := &parityConfig{}
			require.NoError(t, loadFiles(t, &yaml3.Backend{}, fromYAML, map[string]string{"a.yaml": tc.yaml}, loader.WithSkipEnv()))
			fromTOML := &parityConfig{}
			require.NoError(t, loadFiles(t, &toml.Backend{}, fromTOML, map[string]string{"a.toml": tc.toml}, loader.WithSkipEnv()))
			require.Equal(t, fromYAML, fromTOML)
		})
	}
}
