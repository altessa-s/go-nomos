// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"

	loadersecrets "github.com/altessa-s/go-nomos/loader/secrets"
)

// All tests in this file mutate process environment via t.Setenv, which is
// incompatible with t.Parallel — keep them serial.

const regPrefix = "ENVREG"

// loadReg loads cfg from the ENVREG-prefixed environment, in strict mode when
// strict is set, with the section delimiter delimiter ("" keeps the default).
func loadReg(t testing.TB, cfg any, strict bool, delimiter string) error {
	t.Helper()
	opts := []loader.Option{loader.WithEnvPrefix(regPrefix)}
	if strict {
		opts = append(opts, loader.WithStrict())
	}
	if delimiter != "" {
		opts = append(opts, loader.WithEnvSectionDelimiter(delimiter))
	}
	_, err := loader.New(nil, opts...).Load(cfg)
	return err
}

type regItem struct {
	Name    string        `yaml:"name"`
	Timeout time.Duration `yaml:"timeout"`
}

type regSuffixConfig struct {
	Inner struct {
		Ports []int     `yaml:"ports"`
		Items []regItem `yaml:"items"`
	} `yaml:"inner"`
	Ports  []int            `yaml:"ports"`
	Items  []regItem        `yaml:"items"`
	Labels map[string][]int `yaml:"labels"`
}

// TestLoad_EnvLegacySuffixKeepsTrailingLevels checks that a legacy index
// suffix (NAME__0) under a custom delimiter keeps the levels after it: a level
// below a value element is reported in strict mode and ignored otherwise, and
// a struct element field is set.
func TestLoad_EnvLegacySuffixKeepsTrailingLevels(t *testing.T) {
	t.Run("below value element", func(t *testing.T) {
		t.Setenv(regPrefix+"_INNER.PORTS__0.EXTRA", "80")

		require.ErrorIs(t, loadReg(t, &regSuffixConfig{}, true, "."), loader.ErrFieldNotFound)

		cfg := &regSuffixConfig{}
		require.NoError(t, loadReg(t, cfg, false, "."))
		require.Empty(t, cfg.Inner.Ports)
	})
	t.Run("struct element field", func(t *testing.T) {
		t.Setenv(regPrefix+"_INNER.ITEMS__0.NAME", "x")

		for _, strict := range []bool{false, true} {
			cfg := &regSuffixConfig{}
			require.NoError(t, loadReg(t, cfg, strict, "."))
			require.Equal(t, []regItem{{Name: "x"}}, cfg.Inner.Items)
		}
	})
	t.Run("root level", func(t *testing.T) {
		t.Setenv(regPrefix+"_PORTS__0", "80")

		for _, strict := range []bool{false, true} {
			cfg := &regSuffixConfig{}
			require.NoError(t, loadReg(t, cfg, strict, "."))
			require.Equal(t, []int{80}, cfg.Ports)
		}
	})
}

// TestLoad_EnvSuffixOnScalarInElement checks that an index suffix on a
// scalar field inside a slice element is reported in strict mode and ignored
// otherwise.
func TestLoad_EnvSuffixOnScalarInElement(t *testing.T) {
	t.Setenv(regPrefix+"_ITEMS.0.TIMEOUT__0", "1s")

	require.ErrorIs(t, loadReg(t, &regSuffixConfig{}, true, "."), loader.ErrFieldNotFound)

	cfg := &regSuffixConfig{}
	require.NoError(t, loadReg(t, cfg, false, "."))
	for _, item := range cfg.Items {
		require.Zero(t, item.Timeout)
	}
}

// TestLoad_EnvMapKeyVerbatim checks that a map key level is taken verbatim,
// never read as a field with an index suffix.
func TestLoad_EnvMapKeyVerbatim(t *testing.T) {
	t.Setenv(regPrefix+"_LABELS.foo__0.0", "7")

	for _, strict := range []bool{false, true} {
		cfg := &regSuffixConfig{}
		require.NoError(t, loadReg(t, cfg, strict, "."))
		require.Equal(t, map[string][]int{"foo__0": {7}}, cfg.Labels)
	}
}

type regPtrSliceConfig struct {
	Ports  *[]int `yaml:"ports"`
	Tagged *[]int `yaml:"tagged" env:"PORT_LIST"`
}

// TestLoad_EnvPointerToSliceElements checks that elements of a pointer to a
// slice are set: through a nil pointer, an existing slice and an env tag.
func TestLoad_EnvPointerToSliceElements(t *testing.T) {
	t.Setenv(regPrefix+"_PORTS__1", "81")
	t.Setenv(regPrefix+"_PORT_LIST__0", "90")

	for _, strict := range []bool{false, true} {
		cfg := &regPtrSliceConfig{}
		require.NoError(t, loadReg(t, cfg, strict, ""))
		require.NotNil(t, cfg.Ports)
		require.Equal(t, []int{0, 81}, *cfg.Ports)
		require.NotNil(t, cfg.Tagged)
		require.Equal(t, []int{90}, *cfg.Tagged)

		cfg = &regPtrSliceConfig{Ports: &[]int{1, 2, 3}}
		require.NoError(t, loadReg(t, cfg, strict, ""))
		require.Equal(t, []int{1, 81, 3}, *cfg.Ports)
	}
}

type regUnexportedConfig struct {
	Name  string `yaml:"name"`
	ports []int
	inner struct {
		Hosts []string
	}
}

// TestLoad_EnvUnexportedFieldsIgnored checks that unexported fields are not
// addressed from the environment.
func TestLoad_EnvUnexportedFieldsIgnored(t *testing.T) {
	t.Setenv(regPrefix+"_PORTS__0", "80")
	t.Setenv(regPrefix+"_INNER__HOSTS__0", "h")

	cfg := &regUnexportedConfig{}
	require.NoError(t, loadReg(t, cfg, false, ""))
	require.Empty(t, cfg.ports)
	require.Empty(t, cfg.inner.Hosts)
}

type regMapConfig struct {
	Limits map[string]int `yaml:"limits"`
	Items  []struct {
		Limits map[string]int `yaml:"limits"`
	} `yaml:"items"`
	Empty map[string]int `yaml:"empty"`
}

// TestLoad_EnvEmptyMapEntryKeepsValue checks that an empty value for a map
// entry assigns nothing: the existing entry is kept and no map is allocated.
func TestLoad_EnvEmptyMapEntryKeepsValue(t *testing.T) {
	t.Setenv(regPrefix+"_LIMITS__k", "")
	t.Setenv(regPrefix+"_ITEMS__0__LIMITS__k", "")
	t.Setenv(regPrefix+"_EMPTY__k", "")

	for _, strict := range []bool{false, true} {
		cfg := &regMapConfig{Limits: map[string]int{"k": 7}}
		cfg.Items = append(cfg.Items, struct {
			Limits map[string]int `yaml:"limits"`
		}{Limits: map[string]int{"k": 8}})
		require.NoError(t, loadReg(t, cfg, strict, ""))
		require.Equal(t, map[string]int{"k": 7}, cfg.Limits)
		require.Equal(t, map[string]int{"k": 8}, cfg.Items[0].Limits)
		require.Nil(t, cfg.Empty)
	}
}

type regHostsConfig struct {
	Hosts []string `yaml:"hosts"`
}

// TestLoad_EnvBracketAndSuffixSpellingsNameOrder checks that of HOSTS[0] and
// HOSTS__0, which set one element, the first in name order (HOSTS[0]) wins on
// every load.
func TestLoad_EnvBracketAndSuffixSpellingsNameOrder(t *testing.T) {
	t.Setenv(regPrefix+"_HOSTS[0]", "bracket")
	t.Setenv(regPrefix+"_HOSTS__0", "suffix")

	for range 50 {
		cfg := &regHostsConfig{}
		require.NoError(t, loadReg(t, cfg, false, ""))
		require.Equal(t, []string{"bracket"}, cfg.Hosts)
	}
}

type regExpandConfig struct {
	Name  string   `yaml:"name"`
	Hosts []string `yaml:"hosts"`
}

// TestStrict_EnvExpansionOnlyForAssignedVariables checks that strict $VAR
// expansion runs only on the variables the loader assigns: an unrelated
// variable or a losing spelling referencing an undefined variable does not
// fail the load, and the expansion error names the variable.
func TestStrict_EnvExpansionOnlyForAssignedVariables(t *testing.T) {
	t.Run("unrelated variable", func(t *testing.T) {
		t.Setenv(regPrefix+"_UNRELATED", "$ENVREG_MISSING_VAR")
		t.Setenv(regPrefix+"_NAME", "svc")

		cfg := &regExpandConfig{}
		require.NoError(t, loadReg(t, cfg, true, ""))
		require.Equal(t, "svc", cfg.Name)
	})
	t.Run("losing spelling", func(t *testing.T) {
		t.Setenv(regPrefix+"_HOSTS[0]", "a")
		t.Setenv(regPrefix+"_HOSTS__0", "$ENVREG_MISSING_VAR")

		cfg := &regExpandConfig{}
		require.NoError(t, loadReg(t, cfg, true, ""))
		require.Equal(t, []string{"a"}, cfg.Hosts)
	})
	t.Run("assigned variable", func(t *testing.T) {
		t.Setenv(regPrefix+"_NAME", "pre-$ENVREG_MISSING_VAR")

		err := loadReg(t, &regExpandConfig{}, true, "")
		require.ErrorIs(t, err, loader.ErrUndefinedEnvVar)
		require.Contains(t, err.Error(), regPrefix+"_NAME")
	})
}

// TestLoad_EnvWholeReferenceWithSuffix checks that a value made of a variable
// reference followed by other characters expands the reference only, and a
// "$" followed by no name stays literal, in both modes.
func TestLoad_EnvWholeReferenceWithSuffix(t *testing.T) {
	t.Setenv("ENVREG_REF", "service")
	tests := []struct {
		name, value, want string
	}{
		{name: "suffix", value: "$ENVREG_REF-suffix", want: "service-suffix"},
		{name: "bang", value: "$!", want: "$!"},
		{name: "whole", value: "$ENVREG_REF", want: "service"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(regPrefix+"_NAME", tc.value)
			for _, strict := range []bool{false, true} {
				cfg := &regExpandConfig{}
				require.NoError(t, loadReg(t, cfg, strict, ""))
				require.Equal(t, tc.want, cfg.Name)
			}
		})
	}
}

// TestLoad_NilConfig checks that loading into nil fails with
// ErrInvalidConfig instead of panicking.
func TestLoad_NilConfig(t *testing.T) {
	t.Parallel()

	_, err := loader.New(nil, loader.WithSkipEnv()).Load(nil)
	require.ErrorIs(t, err, loader.ErrInvalidConfig)
}

type regTaggedItem struct {
	Ports []int `yaml:"ports"`
}

type regTaggedConfig struct {
	Items  []regTaggedItem `yaml:"items" env:"CUSTOM"`
	Others []regTaggedItem `yaml:"others" env:"OTHER.LIST"`
}

// TestLoad_EnvLegacySuffixUnderTaggedCollection checks that a legacy index
// suffix inside the entries of an env-tagged slice is read relative to the
// tagged field, also when the tag contains the delimiter.
func TestLoad_EnvLegacySuffixUnderTaggedCollection(t *testing.T) {
	t.Setenv(regPrefix+"_CUSTOM.0.PORTS__0", "80")
	t.Setenv(regPrefix+"_OTHER.LIST.0.PORTS__1", "81")

	for _, strict := range []bool{false, true} {
		cfg := &regTaggedConfig{}
		require.NoError(t, loadReg(t, cfg, strict, "."))
		require.Equal(t, []regTaggedItem{{Ports: []int{80}}}, cfg.Items)
		require.Equal(t, []regTaggedItem{{Ports: []int{0, 81}}}, cfg.Others)
	}
}

type regNestedSliceConfig struct {
	Items []struct {
		Ports []int `yaml:"ports"`
		Inner struct {
			Ports []int `yaml:"ports"`
		} `yaml:"inner"`
		Subs []struct {
			Name string `yaml:"name"`
		} `yaml:"subs"`
		Tags map[string][]int `yaml:"tags"`
	} `yaml:"items"`
}

// TestLoad_EnvNestedSliceSecretResolvedOnce checks that a variable below
// several slice indexes is assigned, and its secret resolved, once.
func TestLoad_EnvNestedSliceSecretResolvedOnce(t *testing.T) {
	envs := []string{"ITEMS__0__PORTS__0", "ITEMS__0__INNER__PORTS__0", "ITEMS__0__SUBS__0__NAME", "ITEMS__0__TAGS__k__0"}
	for _, env := range envs {
		t.Run(env, func(t *testing.T) {
			t.Setenv(regPrefix+"_"+env, "$__secret{app:port}")
			var calls atomic.Int32
			resolver := loadersecrets.ResolverFunc(func(context.Context, string) (string, error) {
				if calls.Add(1) > 1 {
					return "", errors.New("resolved twice")
				}
				return "80", nil
			})

			cfg := &regNestedSliceConfig{}
			_, err := loader.New(nil, loader.WithEnvPrefix(regPrefix), loader.WithSecretResolver(resolver)).Load(cfg)
			require.NoError(t, err)
			require.Equal(t, int32(1), calls.Load())
			require.Len(t, cfg.Items, 1)
		})
	}
}

type regPtrMapConfig struct {
	Limits map[string]*int `yaml:"limits"`
	Empty  map[string]*int `yaml:"empty"`
}

// TestLoad_EnvEmptyPointerMapEntryKeepsValue checks that an empty value for
// a pointer-valued map entry assigns nothing: an existing entry is kept, a
// missing one is not created and no map is allocated.
func TestLoad_EnvEmptyPointerMapEntryKeepsValue(t *testing.T) {
	t.Setenv(regPrefix+"_LIMITS__k", "")
	t.Setenv(regPrefix+"_LIMITS__m", "")
	t.Setenv(regPrefix+"_LIMITS__n", "5")
	t.Setenv(regPrefix+"_EMPTY__k", "")

	cfg := &regPtrMapConfig{Limits: map[string]*int{"k": new(7)}}
	require.NoError(t, loadReg(t, cfg, false, ""))
	require.Len(t, cfg.Limits, 2)
	require.Equal(t, 7, *cfg.Limits["k"])
	require.Equal(t, 5, *cfg.Limits["n"])
	require.Nil(t, cfg.Empty)
}
