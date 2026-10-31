// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type pinNavInner struct {
	Name string
}

type pinNav struct {
	Plain   string
	Labels  map[string]string
	Entries map[string]pinNavInner
	Inner   pinNavInner
}

func TestSetNestedFieldValue_Pin(t *testing.T) {
	t.Parallel()

	set := func(t *testing.T, cf *loadState, target reflect.Value, parts ...string) error {
		t.Helper()
		return cf.setNestedFieldValue(t.Context(), &field{value: target}, parts, "v", nil, false)
	}

	t.Run("empty path sets nothing", func(t *testing.T) {
		t.Parallel()
		var c pinNav
		require.NoError(t, set(t, newTestLoadState(t), reflect.ValueOf(&c).Elem()))
		require.Equal(t, pinNav{}, c)
	})

	t.Run("nil map is created for its entry", func(t *testing.T) {
		t.Parallel()
		var c pinNav
		require.NoError(t, set(t, newTestLoadState(t), reflect.ValueOf(&c).Elem(), "LABELS", "k"))
		require.Equal(t, map[string]string{"k": "v"}, c.Labels)
	})

	t.Run("map entry of a nested type is updated in place", func(t *testing.T) {
		t.Parallel()
		c := pinNav{Entries: map[string]pinNavInner{"k": {Name: "old"}}}
		require.NoError(t, set(t, newTestLoadState(t), reflect.ValueOf(&c).Elem(), "ENTRIES", "k", "NAME"))
		require.Equal(t, map[string]pinNavInner{"k": {Name: "v"}}, c.Entries)
	})

	t.Run("a path below a scalar map value sets nothing but creates the map", func(t *testing.T) {
		t.Parallel()
		var c pinNav
		require.NoError(t, set(t, newTestLoadState(t), reflect.ValueOf(&c).Elem(), "LABELS", "k", "deeper"))
		require.NotNil(t, c.Labels)
		require.Empty(t, c.Labels)
	})

	t.Run("a map whose keys a variable cannot spell is left alone", func(t *testing.T) {
		t.Parallel()
		var c struct{ Weird map[[2]int]string }
		require.NoError(t, set(t, newTestLoadState(t), reflect.ValueOf(&c).Elem(), "WEIRD", "k"))
		require.Nil(t, c.Weird)
	})

	t.Run("a path below a scalar field sets nothing", func(t *testing.T) {
		t.Parallel()
		var c pinNav
		require.NoError(t, set(t, newTestLoadState(t), reflect.ValueOf(&c).Elem(), "PLAIN", "deeper"))
		require.Equal(t, pinNav{}, c)
	})

	t.Run("nested struct field is set", func(t *testing.T) {
		t.Parallel()
		var c pinNav
		require.NoError(t, set(t, newTestLoadState(t), reflect.ValueOf(&c).Elem(), "INNER", "NAME"))
		require.Equal(t, "v", c.Inner.Name)
	})

	t.Run("unknown field is ignored unless strict", func(t *testing.T) {
		t.Parallel()
		var c pinNav
		require.NoError(t, set(t, newTestLoadState(t), reflect.ValueOf(&c).Elem(), "MISSING"))
		require.ErrorIs(t, set(t, newTestLoadState(t, WithStrict()), reflect.ValueOf(&c).Elem(), "MISSING"), ErrFieldNotFound)
		require.Equal(t, pinNav{}, c)
	})

	t.Run("an unaddressable final field is left alone", func(t *testing.T) {
		t.Parallel()
		c := pinNav{Plain: "keep"}
		require.NoError(t, set(t, newTestLoadState(t), reflect.ValueOf(c), "PLAIN"))
		require.Equal(t, "keep", c.Plain)
	})
}

func newTestLoadState(t testing.TB, opts ...Option) *loadState {
	t.Helper()
	cfg := New(nil, opts...)
	return &loadState{options: cfg.options, backend: cfg.backend, files: newFiles(), allowExts: make(map[string]struct{})}
}
