// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

func TestBackend_IsFullyCommented(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "empty", data: "", want: false},
		{name: "prose comments only", data: "# just a note\n# another\n", want: false},
		{name: "commented key", data: "#key: value\n", want: true},
		{name: "commented keys and prose", data: "# Settings\n#port: 8080\n  #host: h\n\n", want: true},
		{name: "live key", data: "port: 8080\n", want: false},
		{name: "commented and live keys", data: "#port: 8080\nhost: h\n", want: false},
		{name: "commented line without colon", data: "#port\n", want: false},
		{name: "hash-space key counts as prose", data: "# port: 8080\n", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, (&yaml3.Backend{}).IsFullyCommented([]byte(tc.data)))
		})
	}
}

// A comment marker followed by only blanks is not YAML syntax and is kept;
// a bare "#" becomes an empty line.
func TestBackend_Uncomment_BlankComments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "bare hash", in: "#", want: ""},
		{name: "hash and blanks", in: "#   ", want: "#   "},
		{name: "indented bare hash", in: "  #", want: "  "},
		{name: "prose", in: "# a note", want: "# a note"},
		{name: "list item", in: "#- a", want: "- a"},
		{name: "key value", in: "#  port: 1", want: "  port: 1"},
		{name: "invalid key", in: "#a b: 1", want: "#a b: 1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, string((&yaml3.Backend{}).Uncomment([]byte(tc.in))))
		})
	}
}
