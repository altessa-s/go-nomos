// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/altessa-s/go-nomos/loader/backend"
)

type pinSelfUnmarshaler struct{ X int }

func (*pinSelfUnmarshaler) UnmarshalYAML(*yaml.Node) error { return nil }

type pinInner struct {
	A int
	B int `yaml:"bee"`
}

type pinCycle struct {
	Next *pinCycle `yaml:",inline"`
}

type pinKeys struct {
	unexported int
	Skipped    int `yaml:"-"`
	Plain      int // lowercased
	Bare       int
	Named      int                `yaml:"n,omitempty"`
	Inner      *pinInner          `yaml:",inline"`
	Self       pinSelfUnmarshaler `yaml:",inline"`
	Rest       map[string]any     `yaml:",inline"`
}

func TestBuildStructKeys_Pin(t *testing.T) {
	t.Parallel()

	t.Run("binds keys like yaml.v3", func(t *testing.T) {
		t.Parallel()
		sk, err := buildStructKeys(reflect.TypeFor[pinKeys](), map[reflect.Type]bool{})
		require.NoError(t, err)
		require.Equal(t, map[string][]int{
			"plain": {2},
			"bare":  {3},
			"n":     {4},
			"a":     {5, 0},
			"bee":   {5, 1},
		}, sk.fields)
		require.Equal(t, 7, sk.inlineMap)
	})

	t.Run("a tag without key:value pairs is the yaml tag", func(t *testing.T) {
		t.Parallel()
		// Built at run time: vet's structtag check rejects such a literal tag.
		typ := reflect.StructOf([]reflect.StructField{{Name: "Field", Type: reflect.TypeFor[int](), Tag: "custom"}})
		sk, err := buildStructKeys(typ, map[reflect.Type]bool{})
		require.NoError(t, err)
		require.Equal(t, map[string][]int{"custom": {0}}, sk.fields)
	})

	for _, tc := range []struct {
		name string
		typ  reflect.Type
		err  error
	}{
		{"duplicate plain key", reflect.TypeFor[struct {
			A int `yaml:"k"`
			B int `yaml:"k"`
		}](), errDuplicateKey},
		{"duplicate inlined key", reflect.TypeFor[struct {
			A     int      `yaml:"a"`
			Inner pinInner `yaml:",inline"`
		}](), errDuplicateKey},
		{"inline scalar", reflect.TypeFor[struct {
			N int `yaml:",inline"`
		}](), errBadInline},
		{"inline cycle", reflect.TypeFor[pinCycle](), errBadInline},
		{"inner error", reflect.TypeFor[struct {
			Inner struct {
				N int `yaml:",inline"`
			} `yaml:",inline"`
		}](), errBadInline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := buildStructKeys(tc.typ, map[reflect.Type]bool{})
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestRewriteScalars_Pin(t *testing.T) {
	t.Parallel()

	t.Run("a malformed document fails", func(t *testing.T) {
		t.Parallel()
		_, err := (&Backend{}).RewriteScalars("a: [1\n", func(backend.Scalar) (string, bool, bool, error) {
			return "", false, false, nil
		})
		require.Error(t, err)
	})

	t.Run("an invalid UTF-8 literal is stored as binary", func(t *testing.T) {
		t.Parallel()
		out, err := (&Backend{}).RewriteScalars("a: x\n", func(backend.Scalar) (string, bool, bool, error) {
			return "\xff\xfe", true, true, nil
		})
		require.NoError(t, err)
		require.Equal(t, "a: !!binary //4=\n", out)
	})
}

func TestAliasCounter_NilNode(t *testing.T) {
	t.Parallel()
	c := &aliasCounter{sizes: map[*yaml.Node]int64{}, inProgress: map[*yaml.Node]bool{}}
	s, err := c.size(nil)
	require.NoError(t, err)
	require.Zero(t, s)
	require.NoError(t, c.walk(nil))
	require.Zero(t, c.decodes)
}
