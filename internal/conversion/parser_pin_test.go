// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetNestedValue_Pin(t *testing.T) {
	t.Parallel()

	type kv struct{ key, value string }
	for _, tc := range []struct {
		name     string
		keys     []kv
		want     map[string]any
		conflict bool
	}{
		{"leaf set twice", []kv{{"A__B", "x"}, {"A__B", "y"}}, nil, true},
		{"leaf over a section", []kv{{"A__B", "x"}, {"A", "y"}}, nil, true},
		{"section over a leaf", []kv{{"A", "y"}, {"A__B", "x"}}, nil, true},
		{"index over an untouched placeholder", []kv{{"PORTS__1__HOST", "h"}, {"PORTS__0", "80"}},
			map[string]any{"ports": []any{int64(80), map[string]any{"host": "h"}}}, false},
		{"index over a filled element", []kv{{"PORTS__0__HOST", "h"}, {"PORTS__0", "80"}}, nil, true},
		{"index set twice", []kv{{"PORTS__0", "80"}, {"PORTS__0", "81"}}, nil, true},
		{"field below a scalar element", []kv{{"PORTS__0", "80"}, {"PORTS__0__HOST", "h"}}, nil, true},
		{"array over a section", []kv{{"PORTS__X", "1"}, {"PORTS__0", "80"}}, nil, true},
		{"negative index", []kv{{"PORTS__-1", "80"}}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conv := &Parser{}
			data := map[string]any{}
			var err error
			for _, e := range tc.keys {
				if err = conv.setNestedValue(data, e.key, e.value); err != nil {
					break
				}
			}
			if tc.conflict {
				if tc.name == "negative index" {
					require.ErrorIs(t, err, ErrInvalidInput)
					return
				}
				require.ErrorIs(t, err, ErrConflictingKey)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, data)
		})
	}
}
