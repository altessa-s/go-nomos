// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseNestedValues(t *testing.T) {
	t.Parallel()

	type kv struct{ key, value string }

	for _, tc := range []struct {
		name    string
		keys    []kv
		want    map[string]any
		wantErr error
	}{
		{"leaf set twice", []kv{{"A__B", "x"}, {"A__B", "y"}}, map[string]any{"a": map[string]any{"b": "y"}}, nil},
		{"leaf over a section", []kv{{"A__B", "x"}, {"A", "y"}}, nil, ErrConflictingKey},
		{"section over a leaf", []kv{{"A", "y"}, {"A__B", "x"}}, nil, ErrConflictingKey},
		{"index over an untouched placeholder", []kv{{"PORTS__1__HOST", "h"}, {"PORTS__0", "80"}},
			map[string]any{"ports": []any{int64(80), map[string]any{"host": "h"}}}, nil},
		{"index over a filled element", []kv{{"PORTS__0__HOST", "h"}, {"PORTS__0", "80"}}, nil, ErrConflictingKey},
		{"index set twice", []kv{{"PORTS__0", "80"}, {"PORTS__0", "81"}}, map[string]any{"ports": []any{int64(81)}}, nil},
		{"field below a scalar element", []kv{{"PORTS__0", "80"}, {"PORTS__0__HOST", "h"}}, nil, ErrConflictingKey},
		{"array over a section", []kv{{"PORTS__X", "1"}, {"PORTS__0", "80"}}, nil, ErrConflictingKey},
		{"negative index", []kv{{"PORTS__-1", "80"}}, nil, ErrInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var doc strings.Builder
			for _, e := range tc.keys {
				doc.WriteString(e.key + "=" + e.value + "\n")
			}

			data, err := (&Parser{}).Parse(strings.NewReader(doc.String()), false)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, data)
		})
	}
}
