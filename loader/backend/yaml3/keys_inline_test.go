// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/backend"
)

type inlineMapConfig struct {
	Name  string         `yaml:"name"`
	Inner keysInline     `yaml:",inline"`
	Extra map[string]int `yaml:",inline"`
}

var inlineMapConfigType = reflect.TypeFor[inlineMapConfig]()

// Keys that bind to no field land in the ",inline" map; the inline struct and
// inline map nodes answer only the binding they model.
func TestBackend_DecodeKeys_InlineMap(t *testing.T) {
	t.Parallel()

	root := decodeKeys(t, "name: svc\nhost: h\nx: 1\ny: 2\n")
	fs := fields(t, root, inlineMapConfigType)
	require.Len(t, fs, 3)
	require.Contains(t, fs, 0)

	inline := fs[1]
	require.False(t, inline.IsNull())
	_, ok := inline.Entries(reflect.TypeFor[map[string]int]())
	require.False(t, ok, "an inline struct is not a map")
	_, ok = inline.Elems(reflect.TypeFor[[]int]())
	require.False(t, ok, "an inline struct is not a sequence")
	inlineFields := fields(t, inline, reflect.TypeFor[keysInline]())
	require.Contains(t, inlineFields, 0)

	extra := fs[2]
	require.False(t, extra.IsNull())
	_, ok = extra.Fields(reflect.TypeFor[keysInline]())
	require.False(t, ok, "an inline map is not a struct")
	_, ok = extra.Elems(reflect.TypeFor[[]int]())
	require.False(t, ok, "an inline map is not a sequence")
	es := entries(t, extra, reflect.TypeFor[map[string]int]())
	require.Len(t, es, 2)
	require.Contains(t, es, "x")
	require.Contains(t, es, "y")
}

// Without unknown keys the inline map gets no node.
func TestBackend_DecodeKeys_InlineMapAbsent(t *testing.T) {
	t.Parallel()

	fs := fields(t, decodeKeys(t, "name: svc\n"), inlineMapConfigType)
	require.Len(t, fs, 1)
	require.NotContains(t, fs, 2)
}

// The scalar listers of inline struct and inline map nodes report the scalars
// of their fields and entries.
func TestBackend_DecodeKeys_InlineScalars(t *testing.T) {
	t.Parallel()

	root := decodeKeys(t, "name: svc\nhost: h\nx: 1\ny: 2\n")
	fs := fields(t, root, inlineMapConfigType)

	lines := func(n backend.KeyNode) []int {
		t.Helper()
		l, ok := n.(backend.ScalarLister)
		require.True(t, ok)
		var out []int
		for _, p := range l.Scalars() {
			out = append(out, p.Line)
		}
		slices.Sort(out)
		return out
	}
	require.Equal(t, []int{2}, lines(fs[1]), "host is on line 2")
	require.Equal(t, []int{3, 4}, lines(fs[2]), "x and y are on lines 3 and 4")
}

// A self-unmarshaling type and a type with an invalid inline field do not
// bind field by field.
func TestBackend_DecodeKeys_FieldsRefused(t *testing.T) {
	t.Parallel()

	type badInline struct {
		N int `yaml:",inline"`
	}
	root := decodeKeys(t, "a: 1\n")
	_, ok := root.Fields(reflect.TypeFor[badInline]())
	require.False(t, ok, "an inline int is rejected like yaml.v3 does")

	_, ok = decodeKeys(t, "- a\n").Fields(reflect.TypeFor[keysInline]())
	require.False(t, ok, "a sequence does not bind to a struct")

	empty, ok := decodeKeys(t, "").Fields(reflect.TypeFor[keysInline]())
	require.True(t, ok, "an empty document binds to a struct like yaml.v3 decodes it")
	require.Empty(t, empty)
}
