// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package toml_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/toml"
)

type selfText struct{ S string }

func (s *selfText) UnmarshalText(b []byte) error { s.S = string(b); return nil }

type selfTextMap map[string]int

func (*selfTextMap) UnmarshalText([]byte) error { return nil }

type selfTextSlice []int

func (*selfTextSlice) UnmarshalText([]byte) error { return nil }

// A value binds to a destination type only when the decoder would bind it
// key by key: types with their own unmarshaler and mismatched shapes do not.
func TestBackend_DecodeKeys_Refused(t *testing.T) {
	t.Parallel()

	root, err := (&toml.Backend{}).DecodeKeys(strings.NewReader("table = {a = 1}\narr = [1, 2]\ns = \"x\"\n"))
	require.NoError(t, err)
	type doc struct {
		Table map[string]int `toml:"table"`
		Arr   []int          `toml:"arr"`
		S     string         `toml:"s"`
	}
	fs, ok := root.Fields(reflect.TypeFor[doc]())
	require.True(t, ok)
	table, arr, s := fs[0], fs[1], fs[2]

	tests := []struct {
		name string
		bind func() bool
	}{
		{name: "struct with an unmarshaler", bind: func() bool { _, ok := table.Fields(reflect.TypeFor[selfText]()); return ok }},
		{name: "array into a struct", bind: func() bool { _, ok := arr.Fields(reflect.TypeFor[doc]()); return ok }},
		{name: "map with an unmarshaler", bind: func() bool { _, ok := table.Entries(reflect.TypeFor[selfTextMap]()); return ok }},
		{name: "slice with an unmarshaler", bind: func() bool { _, ok := arr.Elems(reflect.TypeFor[selfTextSlice]()); return ok }},
		{name: "table into a slice", bind: func() bool { _, ok := table.Elems(reflect.TypeFor[[]int]()); return ok }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.False(t, tc.bind())
		})
	}

	es, ok := table.Entries(reflect.TypeFor[map[string]int]())
	require.True(t, ok)
	require.Contains(t, es, "a")
	el, ok := arr.Elems(reflect.TypeFor[[]int]())
	require.True(t, ok)
	require.Len(t, el, 2)
	require.False(t, s.IsNull(), "TOML has no null")
}

func TestBackend_DecodeKeys_InvalidDocument(t *testing.T) {
	t.Parallel()

	var kd backend.KeyDecoder = &toml.Backend{}
	_, err := kd.DecodeKeys(strings.NewReader("a = \n"))
	require.Error(t, err)
}
