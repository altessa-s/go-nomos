// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvToConfig_BuildConfigStructure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want map[string]any
	}{
		{
			name: "array element field",
			env:  map[string]string{"SERVERS__0__HOST": "localhost"},
			want: map[string]any{"servers": []any{map[string]any{"host": "localhost"}}},
		},
		{
			name: "terminal array elements",
			env:  map[string]string{"PORTS__0": "8080", "PORTS__1": "9090"},
			want: map[string]any{"ports": []any{int64(8080), int64(9090)}},
		},
		{
			name: "gap before the indexed element",
			env:  map[string]string{"SERVERS__1__HOST": "db"},
			want: map[string]any{"servers": []any{map[string]any{}, map[string]any{"host": "db"}}},
		},
		{
			name: "array element nested field",
			env:  map[string]string{"SERVERS__0__TLS__ENABLED": "true", "SERVERS__0__PORT": "1"},
			want: map[string]any{"servers": []any{map[string]any{
				"tls":  map[string]any{"enabled": true},
				"port": int64(1),
			}}},
		},
		{
			name: "nested fields",
			env:  map[string]string{"APP__LOG_LEVEL": "debug", "APP__NAME": "svc"},
			want: map[string]any{"app": map[string]any{"logLevel": "debug", "name": "svc"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := (&Parser{}).buildConfigStructure(tc.env)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEnvToConfig_NegativeIndex(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"SERVERS__-1__HOST", "PORTS__-1"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			require.NotPanics(t, func() {
				_, err := (&Parser{}).buildConfigStructure(map[string]string{key: "x"})
				require.Error(t, err)
			})
		})
	}
}
