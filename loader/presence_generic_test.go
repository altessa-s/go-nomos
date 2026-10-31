// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

// plainBackend is a backend that implements only backend.Backend: the
// loader records the keys its documents set from a generic decode.
type plainBackend struct {
	yaml yaml3.Backend
}

func (b *plainBackend) Decode(r io.Reader, in any) error { return b.yaml.Decode(r, in) }
func (b *plainBackend) FileExtensions() []string         { return b.yaml.FileExtensions() }
func (b *plainBackend) StructTagName() string            { return b.yaml.StructTagName() }

type genericItem struct {
	Weight  int  `yaml:"weight" default:"10"`
	Enabled bool `yaml:"enabled" default:"true"`
}

// GenericBase is exported: the fields of an unexported embedded struct are
// not configuration.
type GenericBase struct {
	Level int `yaml:"level" default:"3"`
}

type genericConfig struct {
	GenericBase `yaml:",inline"`

	Name     string                 `yaml:"name" default:"svc"`
	Port     int                    `yaml:"port" default:"8080"`
	Enabled  bool                   `yaml:"enabled" default:"true"`
	CaseName string                 `default:"dflt"`
	Skipped  string                 `yaml:"-" default:"skip"`
	Items    []genericItem          `yaml:"items"`
	ByName   map[string]genericItem `yaml:"byName"`
	ByID     map[int]genericItem    `yaml:"byID"`
	Mixed    map[string]genericItem `yaml:"mixed"`
	Section  genericItem            `yaml:"section"`
}

// A backend without a KeyDecoder still keeps explicit zeros from defaults:
// struct fields bind by tag or, case-insensitively, by field name, an embedded
// struct without a tag name is flattened, sequence elements bind by position, and map
// entries bind by string key. A map with non-string keys is not tracked, so
// its entries take their defaults.
func TestLoad_GenericPresence(t *testing.T) {
	t.Parallel()

	doc := `name: ""
port: 0
enabled: false
level: 0
casename: ""
items:
  - weight: 0
  - enabled: false
byName:
  a:
    weight: 0
byID:
  1:
    weight: 0
mixed:
  1: {weight: 0}
  b: {weight: 0}
section: null
`
	cfg := &genericConfig{}
	require.NoError(t, loadFiles(t, &plainBackend{}, cfg, map[string]string{"a.yaml": doc}, loader.WithSkipEnv()))

	require.Empty(t, cfg.Name)
	require.Zero(t, cfg.Port)
	require.False(t, cfg.Enabled)
	require.Zero(t, cfg.Level, "flattened embedded field keeps its explicit zero")
	require.Empty(t, cfg.CaseName, "case-insensitive field name keeps its explicit zero")
	require.Equal(t, "skip", cfg.Skipped)

	require.Equal(t, []genericItem{{Weight: 0, Enabled: true}, {Weight: 10, Enabled: false}}, cfg.Items)
	require.Equal(t, genericItem{Weight: 0, Enabled: true}, cfg.ByName["a"])
	require.Equal(t, genericItem{Weight: 10, Enabled: true}, cfg.ByID[1], "non-string keys are not tracked")
	require.Equal(t, genericItem{Weight: 0, Enabled: true}, cfg.Mixed["b"])
	require.Equal(t, genericItem{Weight: 10, Enabled: true}, cfg.Section)
}

// An empty mapping through a backend without a KeyDecoder sets nothing, so
// every default applies.
func TestLoad_GenericPresenceEmptyDocument(t *testing.T) {
	t.Parallel()

	cfg := &genericConfig{}
	require.NoError(t, loadFiles(t, &plainBackend{}, cfg, map[string]string{"a.yaml": "{}\n"}, loader.WithSkipEnv()))
	require.Equal(t, "svc", cfg.Name)
	require.Equal(t, 8080, cfg.Port)
	require.True(t, cfg.Enabled)
	require.Equal(t, 3, cfg.Level)
	require.Equal(t, "dflt", cfg.CaseName)
}
