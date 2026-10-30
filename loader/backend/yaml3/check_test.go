// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

// CheckDocument rejects an anchor holding an alias to itself and excessive
// aliasing, by yaml.v3's ratio, and accepts what yaml.v3 decodes: typed keys,
// moderate aliasing, empty documents.
func TestCheckDocument(t *testing.T) {
	t.Parallel()

	var laughs strings.Builder
	laughs.WriteString("a: &a [\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\"]\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		fmt.Fprintf(&laughs, "%s: &%s [*%s,*%s,*%s,*%s,*%s,*%s,*%s,*%s,*%s]\n", name, name, prev, prev, prev, prev, prev, prev, prev, prev, prev)
		prev = name
	}

	b := &yaml3.Backend{}
	require.ErrorContains(t, b.CheckDocument("a: &x\n  b: *x\n"), "contains itself")
	require.ErrorContains(t, b.CheckDocument(laughs.String()), "excessive aliasing")
	require.NoError(t, b.CheckDocument("pairs:\n  ? [a, b]\n  : value\n"))
	require.NoError(t, b.CheckDocument("base: &b {k: v}\none: *b\ntwo: *b\n"))
	require.NoError(t, b.CheckDocument(""))
	require.Error(t, b.CheckDocument("a: [unclosed\n"))
}

// duplicateKeys is 10,000 repetitions of one key. yaml.v3 compares every key
// pair of a mapping and records an error per duplicate pair, ~50M of them.
var duplicateKeys = strings.Repeat("hiddenkey: 0\n", 10_000)

// Every decode path rejects a mapping with repeated keys before yaml.v3's
// pairwise comparison runs, with an error that does not echo the key.
func TestDecodeRejectsDuplicateKeysLinearly(t *testing.T) {
	t.Parallel()

	b := &yaml3.Backend{}
	decoders := map[string]func(string) error{
		"Decode": func(doc string) error {
			var out map[string]any
			return b.Decode(strings.NewReader(doc), &out)
		},
		"DecodeStrict": func(doc string) error {
			var out struct {
				Key int `yaml:"hiddenkey"`
			}
			return b.DecodeStrict(strings.NewReader(doc), &out)
		},
		"DecodeKeys": func(doc string) error {
			_, err := b.DecodeKeys(strings.NewReader(doc))
			return err
		},
		"CheckDocument": b.CheckDocument,
	}
	for name, decode := range decoders {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := decode(duplicateKeys)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "hiddenkey")

			require.Error(t, decode("a: 1\nb: {c: 1, c: 2}\n"))
			require.Error(t, decode("<<: {a: 1}\n<<: {b: 2}\n"))
			require.NoError(t, decode("a: 1\nb: {c: 1, d: 2}\nm: &m {x: 1}\nn:\n  <<: *m\n  x: 2\n"))
		})
	}
}

// Large mappings with unique keys are bounded too: yaml.v3 compares every key
// pair, so total pairwise work grows quadratically with mapping size.
func TestDecodeRejectsExcessiveMappingWork(t *testing.T) {
	t.Parallel()

	var doc strings.Builder
	for i := range 200_000 {
		fmt.Fprintf(&doc, "k%d: 0\n", i)
	}
	var out map[string]any
	require.Error(t, (&yaml3.Backend{}).Decode(strings.NewReader(doc.String()), &out))

	var aliased strings.Builder
	aliased.WriteString("base: &b {")
	for i := range 5_000 {
		fmt.Fprintf(&aliased, "k%d: 0, ", i)
	}
	aliased.WriteString("}\n")
	for i := range 50 {
		fmt.Fprintf(&aliased, "a%d: *b\n", i)
	}
	require.Error(t, (&yaml3.Backend{}).CheckDocument(aliased.String()))
}

// Expansion counts saturate instead of overflowing: a document whose alias
// expansion exceeds 2^41 nodes is rejected as excessive aliasing.
func TestCheckDocument_SaturatesCounts(t *testing.T) {
	t.Parallel()

	var doc strings.Builder
	doc.WriteString("l0: &l0 [x, x]\n")
	for i := 1; i <= 45; i++ {
		fmt.Fprintf(&doc, "l%d: &l%d [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	require.ErrorContains(t, (&yaml3.Backend{}).CheckDocument(doc.String()), "excessive aliasing")
}
