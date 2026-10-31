// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

// All tests in this file mutate the process environment via t.Setenv, which
// is incompatible with t.Parallel — keep them serial.

type envUintConfig struct {
	Workers uint   `yaml:"workers"`
	Small   uint8  `yaml:"small"`
	Count   uint64 `yaml:"count" default:"2"`
}

// Unsigned integer fields bind from the environment; a variable that is not
// an unsigned integer of the field's size fails with ErrUintParse inside
// ErrBindEnv.
func TestLoad_EnvUnsigned(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		t.Setenv("ENVUINT_WORKERS", "8")
		t.Setenv("ENVUINT_SMALL", "255")
		cfg := &envUintConfig{}
		_, err := loader.New(nil, loader.WithEnvPrefix("ENVUINT")).Load(cfg)
		require.NoError(t, err)
		require.Equal(t, envUintConfig{Workers: 8, Small: 255, Count: 2}, *cfg)
	})

	invalid := []struct {
		name, key, value string
	}{
		{name: "negative", key: "ENVUINT_WORKERS", value: "-1"},
		{name: "overflow", key: "ENVUINT_SMALL", value: "256"},
		{name: "not a number", key: "ENVUINT_COUNT", value: "lots"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			_, err := loader.New(nil, loader.WithEnvPrefix("ENVUINT")).Load(&envUintConfig{})
			require.ErrorIs(t, err, loader.ErrBindEnv)
			require.ErrorIs(t, err, loader.ErrUintParse)
		})
	}
}

type envRefConfig struct {
	Value string `yaml:"value"`
}

// loadEnvRef loads ENVREF_VALUE into a fresh envRefConfig.
func loadEnvRef(t testing.TB, opts ...loader.Option) (string, error) {
	t.Helper()
	cfg := &envRefConfig{}
	_, err := loader.New(&yaml3.Backend{}, append([]loader.Option{loader.WithEnvPrefix("ENVREF")}, opts...)...).Load(cfg)
	return cfg.Value, err
}

// $VAR references in environment values expand recursively. A reference
// cycle stops at the first variable seen again, in both modes. A chain of
// whole references longer than the expansion depth keeps the original value;
// a chain through mixed text stops partially expanded at that depth.
func TestLoad_EnvReferenceCycles(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "two-variable cycle",
			env:  map[string]string{"ENVREF_VALUE": "$ENVREF_CYCLE_A", "ENVREF_CYCLE_A": "$ENVREF_CYCLE_B", "ENVREF_CYCLE_B": "$ENVREF_CYCLE_A"},
			want: "$ENVREF_CYCLE_B",
		},
		{
			name: "self reference inside text",
			env:  map[string]string{"ENVREF_VALUE": "$ENVREF_SELF", "ENVREF_SELF": "pre-$ENVREF_SELF"},
			want: "pre-$ENVREF_SELF",
		},
		{
			name: "cycle back to text",
			env:  map[string]string{"ENVREF_VALUE": "$ENVREF_LOOP_A", "ENVREF_LOOP_A": "$ENVREF_LOOP_B", "ENVREF_LOOP_B": "x-$ENVREF_LOOP_A"},
			want: "x-$ENVREF_LOOP_A",
		},
		{
			name: "defined but empty",
			env:  map[string]string{"ENVREF_VALUE": "$ENVREF_EMPTY", "ENVREF_EMPTY": ""},
			want: "",
		},
	}
	for _, tc := range tests {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/strict=%t", tc.name, strict), func(t *testing.T) {
				for k, v := range tc.env {
					t.Setenv(k, v)
				}
				var opts []loader.Option
				if strict {
					opts = append(opts, loader.WithStrict())
				}
				got, err := loadEnvRef(t, opts...)
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			})
		}
	}

	t.Run("chain past the expansion depth", func(t *testing.T) {
		t.Setenv("ENVREF_VALUE", "$ENVREF_CHAIN_0")
		for i := range 12 {
			t.Setenv(fmt.Sprintf("ENVREF_CHAIN_%d", i), fmt.Sprintf("$ENVREF_CHAIN_%d", i+1))
		}
		t.Setenv("ENVREF_CHAIN_12", "end")
		for _, strict := range []bool{false, true} {
			var opts []loader.Option
			if strict {
				opts = append(opts, loader.WithStrict())
			}
			got, err := loadEnvRef(t, opts...)
			require.NoError(t, err)
			require.Equal(t, "$ENVREF_CHAIN_0", got)
		}
	})

	t.Run("mixed text past the expansion depth", func(t *testing.T) {
		t.Setenv("ENVREF_VALUE", "a$ENVREF_MIX_0")
		for i := range 12 {
			t.Setenv(fmt.Sprintf("ENVREF_MIX_%d", i), fmt.Sprintf("b$ENVREF_MIX_%d", i+1))
		}
		t.Setenv("ENVREF_MIX_12", "end")
		for _, strict := range []bool{false, true} {
			var opts []loader.Option
			if strict {
				opts = append(opts, loader.WithStrict())
			}
			got, err := loadEnvRef(t, opts...)
			require.NoError(t, err)
			// Mixed text is expanded one pass per level and kept as far as
			// the depth allows, unlike a whole reference.
			require.Equal(t, "abbbbbbbbbb$ENVREF_MIX_10", got)
		}
	})
}

type envArrayConfig struct {
	Pair  [2]string `yaml:"pair"`
	Names []string  `yaml:"names"`
}

// An array is set whole: an indexed variable naming one of its elements sets
// nothing, and strict mode does not report it as a missing field.
func TestLoad_EnvIndexedArrayIgnored(t *testing.T) {
	t.Setenv("ENVARR_PAIR__1", "second")
	t.Setenv("ENVARR_NAMES__0", "first")
	for _, strict := range []bool{false, true} {
		opts := []loader.Option{loader.WithEnvPrefix("ENVARR")}
		if strict {
			opts = append(opts, loader.WithStrict())
		}
		cfg := &envArrayConfig{}
		_, err := loader.New(nil, opts...).Load(cfg)
		require.NoError(t, err)
		require.Equal(t, [2]string{}, cfg.Pair)
		require.Equal(t, []string{"first"}, cfg.Names)
	}
}

// EnvBase is embedded in envEmbeddedConfig without a tag.
type EnvBase struct {
	Name  string `yaml:"name"`
	Inner *struct {
		Level int `yaml:"level"`
	} `yaml:"inner"`
}

type envEmbeddedConfig struct {
	EnvBase

	Port int `yaml:"port"`
}

// A root-level embedded struct is addressed by its type name as a section;
// a nil pointer below it is allocated only for a variable that sets a field.
func TestLoad_EnvEmbeddedRootSection(t *testing.T) {
	t.Setenv("ENVEMB_ENV_BASE__NAME", "embedded")
	t.Setenv("ENVEMB_ENV_BASE__INNER__LEVEL", "4")
	t.Setenv("ENVEMB_PORT", "81")
	cfg := &envEmbeddedConfig{}
	_, err := loader.New(nil, loader.WithEnvPrefix("ENVEMB")).Load(cfg)
	require.NoError(t, err)
	require.Equal(t, "embedded", cfg.Name)
	require.NotNil(t, cfg.Inner)
	require.Equal(t, 4, cfg.Inner.Level)
	require.Equal(t, 81, cfg.Port)
}

type envNavLeaf struct {
	Level int `yaml:"level"`
}

type envNavEntry struct {
	Inner *envNavLeaf `yaml:"inner"`
}

type envNavConfig struct {
	ByName map[string]envNavEntry `yaml:"byName"`
	List   []envNavEntry          `yaml:"list"`
}

// Navigating into a map entry or a slice element allocates the nil pointer
// sections along a variable's path; in strict mode a level below a value is
// reported as a missing field.
func TestLoad_EnvNavigatesNilPointerSections(t *testing.T) {
	t.Run("allocates", func(t *testing.T) {
		t.Setenv("ENVNAV_BY_NAME__alpha__INNER__LEVEL", "5")
		t.Setenv("ENVNAV_LIST__0__INNER__LEVEL", "6")
		cfg := &envNavConfig{}
		_, err := loader.New(nil, loader.WithEnvPrefix("ENVNAV")).Load(cfg)
		require.NoError(t, err)
		require.NotNil(t, cfg.ByName["alpha"].Inner)
		require.Equal(t, 5, cfg.ByName["alpha"].Inner.Level)
		require.Len(t, cfg.List, 1)
		require.NotNil(t, cfg.List[0].Inner)
		require.Equal(t, 6, cfg.List[0].Inner.Level)
	})
	t.Run("strict level below a value", func(t *testing.T) {
		t.Setenv("ENVNAV_BY_NAME__alpha__INNER__LEVEL__EXTRA", "5")
		_, err := loader.New(nil, loader.WithEnvPrefix("ENVNAV"), loader.WithStrict()).Load(&envNavConfig{})
		require.ErrorIs(t, err, loader.ErrBindEnv)
		require.ErrorIs(t, err, loader.ErrFieldNotFound)
	})
}
