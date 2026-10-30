// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergePresence_Pin(t *testing.T) {
	t.Parallel()

	set := func() *presence { return &presence{set: true} }
	null := func() *presence { return &presence{set: true, null: true} }
	type pair struct{ A, B int }

	t.Run("absent and null operands", func(t *testing.T) {
		t.Parallel()
		dst, src := set(), set()
		intT, ptrT := reflect.TypeFor[int](), reflect.TypeFor[*int]()
		require.Same(t, dst, mergePresence(dst, nil, intT, false))
		require.Same(t, src, mergePresence(nil, src, intT, false))
		n := null()
		require.Same(t, n, mergePresence(dst, n, ptrT, false), "a null resets a pointer")
		require.Same(t, dst, mergePresence(dst, n, intT, false), "a null leaves an int alone")
		require.Same(t, src, mergePresence(n, src, intT, false))
		require.Same(t, src, mergePresence(dst, src, intT, false), "scalars are replaced")
	})

	t.Run("struct", func(t *testing.T) {
		t.Parallel()
		typ := reflect.TypeFor[pair]()
		src := set()
		require.Same(t, src, mergePresence(&presence{set: true, fields: map[int]*presence{0: set()}}, src, typ, false))

		a, b := set(), set()
		got := mergePresence(set(), &presence{set: true, fields: map[int]*presence{1: b}}, typ, false)
		require.Equal(t, &presence{set: true, fields: map[int]*presence{1: b}}, got)

		got = mergePresence(&presence{set: true, fields: map[int]*presence{0: a}}, &presence{set: true, fields: map[int]*presence{1: b}}, typ, false)
		require.Equal(t, &presence{set: true, fields: map[int]*presence{0: a, 1: b}}, got)
	})

	t.Run("map", func(t *testing.T) {
		t.Parallel()
		typ := reflect.TypeFor[map[string]int]()
		src := set()
		require.Same(t, src, mergePresence(&presence{set: true, entries: map[any]*presence{"a": set()}}, src, typ, false))

		b := set()
		got := mergePresence(set(), &presence{set: true, entries: map[any]*presence{"b": b}}, typ, false)
		require.Equal(t, &presence{set: true, entries: map[any]*presence{"b": b}}, got)

		a, nb := set(), null()
		got = mergePresence(
			&presence{set: true, entries: map[any]*presence{"a": a}},
			&presence{set: true, entries: map[any]*presence{"a": null(), "b": nb}}, typ, false)
		require.Equal(t, &presence{set: true, entries: map[any]*presence{"a": a, "b": nb}}, got,
			"a null keeps an existing int entry and adds a new one")

		ptrMap := reflect.TypeFor[map[string]*int]()
		na := null()
		got = mergePresence(
			&presence{set: true, entries: map[any]*presence{"a": a}},
			&presence{set: true, entries: map[any]*presence{"a": na}}, ptrMap, false)
		require.Equal(t, &presence{set: true, entries: map[any]*presence{"a": na}}, got)
	})

	t.Run("slice", func(t *testing.T) {
		t.Parallel()
		typ := reflect.TypeFor[[]int]()
		e0, e1, e2 := set(), set(), set()
		dst := &presence{set: true, elems: []*presence{e0, e1}, spare: []*presence{e2}}
		long := &presence{set: true, elems: []*presence{set(), set(), set(), set()}}
		require.Same(t, long, mergePresence(dst, long, typ, true), "longer than the backing array")
		short := &presence{set: true, elems: []*presence{set()}}
		require.Same(t, short, mergePresence(dst, short, typ, false), "slices are replaced without reuse")
		noElems := set()
		require.Same(t, noElems, mergePresence(dst, noElems, typ, true), "no elements")

		n0 := set()
		got := mergePresence(dst, &presence{set: true, elems: []*presence{n0}}, typ, true)
		require.Equal(t, &presence{set: true, elems: []*presence{n0}, spare: []*presence{e1, e2}}, got)
	})

	t.Run("array", func(t *testing.T) {
		t.Parallel()
		typ := reflect.TypeFor[[3]pair]()
		d0, d1 := &presence{set: true, fields: map[int]*presence{0: set()}}, set()
		s0 := &presence{set: true, fields: map[int]*presence{1: set()}}
		s1, s2 := set(), set()
		dst := &presence{set: true, elems: []*presence{d0, d1}}
		src := &presence{set: true, elems: []*presence{s0, s1, s2}}

		got := mergePresence(dst, src, typ, false)
		require.Equal(t, &presence{set: true, elems: []*presence{s0, s1, s2}}, got)

		got = mergePresence(dst, src, typ, true)
		require.Equal(t, &presence{set: true, elems: []*presence{
			{set: true, fields: map[int]*presence{0: d0.fields[0], 1: s0.fields[1]}}, s1, s2,
		}}, got)
	})
}

type pinSnap struct {
	Exported int
	hidden   int
}

func TestTakeSnapshot_Pin(t *testing.T) {
	t.Parallel()

	t.Run("a whole value never written is not snapshotted", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, takeSnapshot(reflect.ValueOf(1), &presence{}))
	})

	t.Run("a nil pointer to a partly written struct is not snapshotted", func(t *testing.T) {
		t.Parallel()
		var p *pinSnap
		require.Nil(t, takeSnapshot(reflect.ValueOf(p), &presence{set: true, fields: map[int]*presence{0: {set: true}}}))
	})

	t.Run("unexported fields are skipped", func(t *testing.T) {
		t.Parallel()
		v := pinSnap{Exported: 1, hidden: 2}
		s := takeSnapshot(reflect.ValueOf(v), &presence{set: true, fields: map[int]*presence{0: {set: true}, 1: {set: true}}})
		require.NotNil(t, s)
		require.Len(t, s.fields, 1)
		require.Equal(t, 1, s.fields[0].value.Interface())
	})

	t.Run("map keys of another type are skipped", func(t *testing.T) {
		t.Parallel()
		v := map[string]int{"a": 1}
		s := takeSnapshot(reflect.ValueOf(v), &presence{set: true, entries: map[any]*presence{
			nil: {set: true}, struct{}{}: {set: true}, "a": {set: true},
		}})
		require.NotNil(t, s)
		require.Len(t, s.entries, 1)
		require.Equal(t, "a", s.entries[0].key.Interface())
	})
}
