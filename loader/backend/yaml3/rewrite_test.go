// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

// Every scalar node is offered once (an anchor once for all its aliases),
// mapping keys never; literal values stay strings and others resolve their
// type.
func TestRewriteScalars(t *testing.T) {
	t.Parallel()

	content := "key: &a value\nalias: *a\nport: p\nlist:\n  - *a\n  - x\n"
	var offered []string
	out, err := (&yaml3.Backend{}).RewriteScalars(content, func(sc backend.Scalar) (string, bool, bool, error) {
		offered = append(offered, sc.Value)
		require.Equal(t, sc.Value == "value", sc.Shared)
		switch sc.Value {
		case "value":
			return "null", true, true, nil
		case "p":
			return "8080", false, true, nil
		default:
			return "", false, false, nil
		}
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"value", "p", "x"}, offered)

	var got struct {
		Key   string   `yaml:"key"`
		Alias string   `yaml:"alias"`
		Port  int      `yaml:"port"`
		List  []string `yaml:"list"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(out), &got))
	require.Equal(t, "null", got.Key)
	require.Equal(t, "null", got.Alias)
	require.Equal(t, 8080, got.Port)
	require.Equal(t, []string{"null", "x"}, got.List)
}

// Content without a rewritten scalar is returned as it is, and a rewrite
// error stops the walk.
func TestRewriteScalars_UnchangedAndError(t *testing.T) {
	t.Parallel()

	b := &yaml3.Backend{}
	content := "# comment\nkey: value\n"
	out, err := b.RewriteScalars(content, func(backend.Scalar) (string, bool, bool, error) {
		return "", false, false, nil
	})
	require.NoError(t, err)
	require.Equal(t, content, out)

	errStop := errors.New("stop")
	_, err = b.RewriteScalars(content, func(backend.Scalar) (string, bool, bool, error) {
		return "", false, false, errStop
	})
	require.ErrorIs(t, err, errStop)

	empty, err := b.RewriteScalars("", nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

// Scalars lists the positions of the scalar values below a node, following
// aliases, and they match the positions RewriteScalars offers.
func TestKeyNode_Scalars(t *testing.T) {
	t.Parallel()

	b := &yaml3.Backend{}
	content := "a: &x one\nb:\n  c: *x\n  d: [two, three]\n"
	root, err := b.DecodeKeys(strings.NewReader(content))
	require.NoError(t, err)

	type inner struct {
		C string   `yaml:"c"`
		D []string `yaml:"d"`
	}
	fields, ok := root.Fields(reflect.TypeFor[struct {
		A string `yaml:"a"`
		B inner  `yaml:"b"`
	}]())
	require.True(t, ok)
	lister, ok := fields[1].(backend.ScalarLister)
	require.True(t, ok)

	offered := map[backend.Position]string{}
	_, err = b.RewriteScalars(content, func(sc backend.Scalar) (string, bool, bool, error) {
		offered[sc.Pos] = sc.Value
		return "", false, false, nil
	})
	require.NoError(t, err)

	var values []string
	for _, pos := range lister.Scalars() {
		values = append(values, offered[pos])
	}
	require.ElementsMatch(t, []string{"one", "two", "three"}, values)
}

// A scalar is offered with its decoded text (base64 decoded for !!binary)
// and its explicit tag, if any.
func TestRewriteScalars_DecodedTextAndTag(t *testing.T) {
	t.Parallel()

	content := "a: !!binary aGVsbG8=\nb: !!str x\nc: plain\nd: !custom y\n"
	offered := map[string]backend.Scalar{}
	_, err := (&yaml3.Backend{}).RewriteScalars(content, func(sc backend.Scalar) (string, bool, bool, error) {
		offered[sc.Value] = sc
		return "", false, false, nil
	})
	require.NoError(t, err)
	require.Equal(t, "!!binary", offered["hello"].Tag)
	require.Equal(t, "!!str", offered["x"].Tag)
	require.Empty(t, offered["plain"].Tag)
	require.Equal(t, "!custom", offered["y"].Tag)
}
