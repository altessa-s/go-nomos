// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHoldsPlaceholder_Pin(t *testing.T) {
	t.Parallel()

	const ph = "$__secret{vault:db/password}"
	for _, tc := range []struct {
		name string
		v    any
		want bool
	}{
		{"string", ph, true},
		{"plain string", "x", false},
		{"bytes", []byte(ph), true},
		{"string map", map[string]any{"a": "x", "b": ph}, true},
		{"string map without", map[string]any{"a": "x"}, false},
		{"any map", map[any]any{1: ph}, true},
		{"any map key only", map[any]any{ph: "x"}, false},
		{"list", []any{"x", ph}, true},
		{"typed map", map[string]string{"a": ph}, true},
		{"typed map without", map[int]string{1: "x"}, false},
		{"typed slice", []string{"x", ph}, true},
		{"array", [2]string{"x", "y"}, false},
		{"nested", map[string]any{"a": []any{map[any]any{"k": []string{ph}}}}, true},
		{"number", 42, false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, holdsPlaceholder(tc.v))
		})
	}
}
