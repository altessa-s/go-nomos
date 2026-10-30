// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
)

// aliasedDefaulter overwrites Value with a fallback and counts its Default
// calls.
type aliasedDefaulter struct {
	Value int
	Label string `default:"fallback"`
	calls int
}

func (d *aliasedDefaulter) Default() {
	d.calls++
	d.Value = 9
}

// aliasedDefaultsConfig reaches one struct through two fields.
type aliasedDefaultsConfig struct {
	First  *aliasedDefaulter
	Second *aliasedDefaulter
}

// A struct reached through two fields gets its defaults once, and a value
// the environment set through either field is kept, whichever field comes
// first. It mutates the environment, so it is not parallel.
func TestDefaultsRunOnceForAliasedStructs(t *testing.T) {
	for _, field := range []string{"FIRST", "SECOND"} {
		t.Run(field, func(t *testing.T) {
			t.Setenv("COREALIASTEST_"+field+"__VALUE", "5")
			t.Setenv("COREALIASTEST_"+field+"__LABEL", "")
			shared := &aliasedDefaulter{}
			cfg := &aliasedDefaultsConfig{First: shared, Second: shared}
			_, err := loader.New(nil, loader.WithEnvPrefix("COREALIASTEST")).LoadContext(t.Context(), cfg)
			require.NoError(t, err)
			require.Same(t, cfg.First, cfg.Second)
			require.Equal(t, 1, shared.calls)
			require.Equal(t, 5, shared.Value)
			require.Empty(t, shared.Label, "an explicitly empty value keeps its value")
		})
	}
}

// aliasedItem is a map value of aliasedCollections.
type aliasedItem struct {
	X, Y int
}

// aliasedCollections replaces its collections in Default.
type aliasedCollections struct {
	Ports []int
	Items map[string]*aliasedItem
}

func (c *aliasedCollections) Default() {
	c.Ports = []int{9, 9}
	for _, it := range c.Items {
		it.X, it.Y = 9, 9
	}
}

// aliasedCollectionsConfig reaches one struct through two fields.
type aliasedCollectionsConfig struct {
	First  *aliasedCollections
	Second *aliasedCollections
}

// Explicit slice elements and map entry fields set through different fields
// reaching one struct are all kept. It mutates the environment, so it is not
// parallel.
func TestDefaultsKeepDisjointExplicitValuesOfAliases(t *testing.T) {
	t.Setenv("COREALIASCOLLTEST_FIRST__PORTS__0", "0")
	t.Setenv("COREALIASCOLLTEST_SECOND__PORTS__1", "0")
	t.Setenv("COREALIASCOLLTEST_FIRST__ITEMS__a__X", "0")
	t.Setenv("COREALIASCOLLTEST_SECOND__ITEMS__a__Y", "0")
	shared := &aliasedCollections{Ports: []int{1, 2}, Items: map[string]*aliasedItem{"a": {X: 1, Y: 2}}}
	cfg := &aliasedCollectionsConfig{First: shared, Second: shared}
	_, err := loader.New(nil, loader.WithEnvPrefix("COREALIASCOLLTEST")).LoadContext(t.Context(), cfg)
	require.NoError(t, err)
	require.Equal(t, []int{0, 0}, shared.Ports)
	require.Equal(t, aliasedItem{}, *shared.Items["a"])
}
