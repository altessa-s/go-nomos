// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
)

type copyLeaf struct {
	Name string `yaml:"name"`
}

// copyConfig holds values a file sets whole, which Default() then mutates in
// place through shared references.
type copyConfig struct {
	Extra any         `yaml:"extra"`
	Pair  [2]string   `yaml:"pair"`
	Leaf  copyLeaf    `yaml:"leaf"`
	Opt   *copyLeaf   `yaml:"opt"`
	Tags  []string    `yaml:"tags"`
	Shape map[int]int `yaml:"shape"`
}

// Default mutates every value it can reach in place, and replaces the rest.
func (c *copyConfig) Default() {
	if m, ok := c.Extra.(map[string]any); ok {
		m["name"] = "mutated"
		if inner, ok := m["inner"].(map[string]any); ok {
			inner["level"] = "mutated"
		}
		if list, ok := m["list"].([]any); ok && len(list) > 0 {
			list[0] = "mutated"
		}
	}
	c.Pair = [2]string{"default-a", "default-b"}
	c.Leaf = copyLeaf{Name: "default"}
	c.Opt = &copyLeaf{Name: "default"}
	c.Tags = []string{"default"}
	c.Shape = map[int]int{1: 1}
}

// The values a file set are snapshotted as independent deep copies before
// Default runs: mutating a decoded map, slice or nested value in place, or
// replacing an explicit null, does not change what the file set.
func TestLoad_DefaulterCannotMutateExplicitValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		doc   string
		check func(t *testing.T, cfg *copyConfig)
	}{
		{
			name: "interface holding nested maps and lists",
			doc:  "extra:\n  name: file\n  inner:\n    level: file\n  list: [file, second]\n",
			check: func(t *testing.T, cfg *copyConfig) {
				require.Equal(t, map[string]any{
					"name":  "file",
					"inner": map[string]any{"level": "file"},
					"list":  []any{"file", "second"},
				}, cfg.Extra)
			},
		},
		{
			name: "explicit nulls",
			doc:  "extra: null\npair: null\nleaf: null\nopt: null\ntags: null\nshape: null\n",
			check: func(t *testing.T, cfg *copyConfig) {
				require.Nil(t, cfg.Extra)
				require.Equal(t, [2]string{}, cfg.Pair)
				require.Equal(t, copyLeaf{}, cfg.Leaf)
				require.Nil(t, cfg.Opt)
				require.Nil(t, cfg.Tags)
				require.Nil(t, cfg.Shape)
			},
		},
		{
			name: "explicitly empty collections",
			doc:  "tags: []\nshape: {}\n",
			check: func(t *testing.T, cfg *copyConfig) {
				require.Empty(t, cfg.Tags)
				require.NotNil(t, cfg.Tags)
				require.Empty(t, cfg.Shape)
				require.NotNil(t, cfg.Shape)
			},
		},
		{
			name: "omitted values take Default",
			doc:  "",
			check: func(t *testing.T, cfg *copyConfig) {
				require.Equal(t, [2]string{"default-a", "default-b"}, cfg.Pair)
				require.Equal(t, "default", cfg.Leaf.Name)
				require.Equal(t, &copyLeaf{Name: "default"}, cfg.Opt)
				require.Equal(t, []string{"default"}, cfg.Tags)
				require.Equal(t, map[int]int{1: 1}, cfg.Shape)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &copyConfig{}
			require.NoError(t, loadFiles(t, nil, cfg, map[string]string{"a.yaml": tc.doc}, loader.WithSkipEnv()))
			tc.check(t, cfg)
		})
	}
}

// cyclicName decodes from a scalar into a value that points to itself, so
// the file sets it whole and its snapshot holds a pointer cycle.
type cyclicName struct {
	Name string
	Self *cyclicName
}

// UnmarshalText stores the text and links the value to itself.
func (c *cyclicName) UnmarshalText(b []byte) error {
	c.Name = string(b)
	c.Self = c
	return nil
}

type cyclicConfig struct {
	Cycle cyclicName `yaml:"cycle"`
}

// Default rewrites the name through the cycle.
func (c *cyclicConfig) Default() {
	if c.Cycle.Self != nil {
		c.Cycle.Self.Name = "mutated"
	}
	c.Cycle.Name = "mutated"
}

// A value set whole by a file is snapshotted as a deep copy that keeps its
// pointer cycle a cycle: Default writing through the cycle does not reach
// the snapshot, and the restored value still points to itself.
func TestLoad_DefaulterSnapshotCopiesPointerCycles(t *testing.T) {
	t.Parallel()

	cfg := &cyclicConfig{}
	require.NoError(t, loadFiles(t, nil, cfg, map[string]string{"a.yaml": "cycle: file\n"}, loader.WithSkipEnv()))
	require.Equal(t, "file", cfg.Cycle.Name)
	require.NotNil(t, cfg.Cycle.Self)
	require.Equal(t, "file", cfg.Cycle.Self.Name)
	require.Same(t, cfg.Cycle.Self, cfg.Cycle.Self.Self)
}

type uintConfig struct {
	Workers uint    `yaml:"workers" default:"4"`
	Small   uint8   `yaml:"small" default:"200"`
	Wide    uint64  `yaml:"wide" default:"18446744073709551615"`
	Nil     *uint32 `yaml:"nil" default:"7"`
	Zero    *uint32 `yaml:"zero" default:"7"`
	Set     *uint32 `yaml:"set" default:"7"`
	Kept    uint16  `yaml:"kept" default:"9"`
}

// Unsigned integer fields take their default tags; an explicit zero and a
// value the caller passed in are kept. A default tag fills the zero value a
// non-nil pointer the caller passed in points to, and leaves a nil scalar
// pointer nil.
func TestLoad_UnsignedDefaults(t *testing.T) {
	t.Parallel()

	cfg := &uintConfig{Kept: 3, Zero: new(uint32(0)), Set: new(uint32(5))}
	require.NoError(t, loadFiles(t, nil, cfg, map[string]string{"a.yaml": "small: 0\n"}, loader.WithSkipEnv()))
	require.Equal(t, uint(4), cfg.Workers)
	require.Zero(t, cfg.Small)
	require.Equal(t, uint64(18446744073709551615), cfg.Wide)
	require.Nil(t, cfg.Nil)
	require.Equal(t, uint32(7), *cfg.Zero)
	require.Equal(t, uint32(5), *cfg.Set)
	require.Equal(t, uint16(3), cfg.Kept)
}

// A default tag that is not an unsigned integer of the field's size fails
// the load with ErrUintParse inside ErrBindDefaults.
func TestLoad_UnsignedDefaultErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  any
	}{
		{name: "negative", cfg: &struct {
			N uint `default:"-1"`
		}{}},
		{name: "overflow", cfg: &struct {
			N uint8 `default:"256"`
		}{}},
		{name: "not a number", cfg: &struct {
			N uint16 `default:"many"`
		}{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := loader.New(nil, loader.WithSkipEnv()).Load(tc.cfg)
			require.ErrorIs(t, err, loader.ErrBindDefaults)
			require.ErrorIs(t, err, loader.ErrUintParse)
		})
	}
}
