// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
)

// These tests set process environment with t.Setenv, which is incompatible
// with t.Parallel — keep them serial. They pin how an indexed environment
// variable reaches a slice through slice elements, map entries and nil
// pointers on the way.

type navLeaf struct {
	Tags []string `yaml:"tags"`
}

type navRoot struct {
	Items   []*navLeaf          `yaml:"items"`
	ByValue map[string]navLeaf  `yaml:"byValue"`
	ByPtr   map[string]*navLeaf `yaml:"byPtr"`
	Section *struct {
		Inner *navLeaf `yaml:"inner"`
	} `yaml:"section"`
}

func TestLoad_EnvSlice_ThroughNilSliceElement(t *testing.T) {
	t.Setenv("NAV_ITEMS__0__TAGS__0", "a")
	t.Setenv("NAV_ITEMS__0__TAGS__1", "b")

	cfg := &navRoot{Items: []*navLeaf{nil}}
	_, err := loader.New(nil, loader.WithEnvPrefix("NAV")).Load(cfg)
	require.NoError(t, err)

	require.Len(t, cfg.Items, 1)
	require.NotNil(t, cfg.Items[0])
	require.Equal(t, []string{"a", "b"}, cfg.Items[0].Tags)
}

func TestLoad_EnvSlice_ThroughMapEntries(t *testing.T) {
	t.Setenv("NAV_BY_VALUE__east__TAGS__0", "v0")
	t.Setenv("NAV_BY_VALUE__east__TAGS__1", "v1")
	t.Setenv("NAV_BY_PTR__west__TAGS__0", "p0")

	cfg := &navRoot{ByValue: map[string]navLeaf{"east": {Tags: []string{"old"}}}}
	_, err := loader.New(nil, loader.WithEnvPrefix("NAV")).Load(cfg)
	require.NoError(t, err)

	require.Equal(t, []string{"v0", "v1"}, cfg.ByValue["east"].Tags, "a by-value entry is updated and stored back")
	require.NotNil(t, cfg.ByPtr["west"])
	require.Equal(t, []string{"p0"}, cfg.ByPtr["west"].Tags)
}

func TestLoad_EnvSlice_ThroughNilPointerSections(t *testing.T) {
	t.Setenv("NAV_SECTION__INNER__TAGS__0", "s0")

	cfg := &navRoot{}
	_, err := loader.New(nil, loader.WithEnvPrefix("NAV")).Load(cfg)
	require.NoError(t, err)

	require.NotNil(t, cfg.Section)
	require.NotNil(t, cfg.Section.Inner)
	require.Equal(t, []string{"s0"}, cfg.Section.Inner.Tags)
}
