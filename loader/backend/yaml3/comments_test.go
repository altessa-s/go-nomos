// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

func TestBackend_CommentPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  string
		want [][]string
	}{
		{name: "empty document", doc: "", want: nil},
		{name: "no comments", doc: "a: 1\nb:\n  c: 2\n", want: nil},
		{
			name: "line comment on a top-level value",
			doc:  "a: 1 # the a\nb: 2\n",
			want: [][]string{{"a"}},
		},
		{
			name: "line comment on a nested value",
			doc:  "server:\n  port: 8080\n  host: h # the host\nother: x\n",
			want: [][]string{{"server", "host"}},
		},
		{
			name: "sequences are not descended",
			doc:  "list:\n  - a # one\n  - b\n",
			want: nil,
		},
		{
			name: "non-mapping root",
			doc:  "- a # one\n",
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := (&yaml3.Backend{}).CommentPaths([]byte(tc.doc))
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// Sibling paths never share a backing array.
func TestBackend_CommentPaths_PathsAreIndependent(t *testing.T) {
	t.Parallel()

	doc := "root:\n  mid:\n    a: 1 # a\n    b: 2 # b\n    c: 3 # c\n"
	got, err := (&yaml3.Backend{}).CommentPaths([]byte(doc))
	require.NoError(t, err)
	require.Equal(t, [][]string{{"root", "mid", "a"}, {"root", "mid", "b"}, {"root", "mid", "c"}}, got)
}

// yaml.v3 attaches a head comment, and the line comment after "key:" of a
// nested mapping, to the key node: CommentPaths reports those paths too.
func TestBackend_CommentPaths_KeyComments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  string
		want [][]string
	}{
		{name: "head comment", doc: "server:\n  # listen port\n  port: 8080\n", want: [][]string{{"server", "port"}}},
		{name: "comment after a mapping key", doc: "db: # database\n  name: n\n", want: [][]string{{"db"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := (&yaml3.Backend{}).CommentPaths([]byte(tc.doc))
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestBackend_CommentPaths_InvalidYAML(t *testing.T) {
	t.Parallel()

	_, err := (&yaml3.Backend{}).CommentPaths([]byte("a: [unclosed\n"))
	require.Error(t, err)
}
