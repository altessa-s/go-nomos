// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvToConfig_Parse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  string
		want map[string]any
	}{
		{
			name: "array element field",
			env:  "SERVERS__0__HOST=localhost",
			want: map[string]any{"servers": []any{map[string]any{"host": "localhost"}}},
		},
		{
			name: "terminal array elements",
			env:  "PORTS__0=8080\nPORTS__1=9090",
			want: map[string]any{"ports": []any{int64(8080), int64(9090)}},
		},
		{
			name: "gap before the indexed element",
			env:  "SERVERS__1__HOST=db",
			want: map[string]any{"servers": []any{map[string]any{}, map[string]any{"host": "db"}}},
		},
		{
			name: "array element nested field",
			env:  "SERVERS__0__TLS__ENABLED=true\nSERVERS__0__PORT=1",
			want: map[string]any{"servers": []any{map[string]any{
				"tls":  map[string]any{"enabled": true},
				"port": int64(1),
			}}},
		},
		{
			name: "nested fields",
			env:  "APP__LOG_LEVEL=debug\nAPP__NAME=svc",
			want: map[string]any{"app": map[string]any{"logLevel": "debug", "name": "svc"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := (&Parser{}).Parse(strings.NewReader(tc.env), false)
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
				_, err := (&Parser{}).Parse(strings.NewReader(key+"=x"), false)
				require.Error(t, err)
			})
		})
	}
}

func TestEnvToConfig_EmptyFieldName(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"", ".A", "A.", "A____B", "A__0___"} {
		_, err := (&Parser{}).Parse(strings.NewReader(key+"=x"), false)
		require.ErrorIs(t, err, ErrInvalidInput, "key %q", key)
	}
}

func TestParseCollidingKeys(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, first, second string
		wantErr             error
	}{
		{"case", "A=1", "a=2", ErrConflictingKey},
		{"delimiters", "A.B=1", "A__B=2", ErrConflictingKey},
		{"underscore", "A_1=1", "A1=2", ErrConflictingKey},
		{"index spelling", "PORTS__00=80", "PORTS__0=81", ErrConflictingKey},
		{"Unicode leaves", "ς=1", "σ=2", ErrConflictingKey},
		{"leading underscore", "_A=1", "A=2", ErrConflictingKey},
		{"nested underscore", "FOO___BAR=1", "FOO__BAR=2", ErrConflictingKey},
		{"leaf and section", "_A=1", "A__B=2", ErrConflictingKey},
		{"Unicode sections", "ς__A=1", "σ__B=2", ErrConflictingKey},
		{"Unicode leaf and section", "ς=1", "σ__B=2", ErrConflictingKey},
		{"whole value and child", "A=1", "A__B=2", ErrConflictingKey},
		{"section and array", "A__B=1", "A__0=2", ErrConflictingKey},
		{"array literal and index", "A=[]", "A__0=2", ErrConflictingKey},
		{"empty initial segment", "A=1A", ".A=1", ErrInvalidInput},
		{"empty final field", "A=1", "A__B__=2", ErrInvalidInput},
		{"empty field after index", "A=1", "A__0__=2", ErrInvalidInput},
		{"negative nested index", "A=1", "A__B__-1=2", ErrInvalidInput},
		{"out of range", "A=1", "A__1024=2", ErrInvalidInput},
		{"integer overflow", "A=1", "A__9999999999999999999999=2", ErrInvalidInput},
		{"invalid UTF-8", "\xba=8", "\x8b=1", ErrInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, doc := range []string{tc.first + "\n" + tc.second, tc.second + "\n" + tc.first} {
				_, err := (&Parser{}).Parse(strings.NewReader(doc), false)
				require.ErrorIs(t, err, tc.wantErr, "%q", doc)
			}
		})
	}
}

func TestExactRepeat(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		doc  string
		want int64
	}{
		{"A=1\nA=2", 2},
		{"A=2\nA=1", 1},
	} {
		data, err := (&Parser{}).Parse(strings.NewReader(tc.doc), false)
		require.NoError(t, err)
		require.Equal(t, map[string]any{"a": tc.want}, data)
	}
}

func TestNormalizedRenderRepeat(t *testing.T) {
	t.Parallel()

	p := &Parser{}
	env := &Env{}
	data := map[string]any{"app": map[string]any{"logLevel": "first", "log_level": "last"}}

	for range 50 {
		_, err := p.Parse(strings.NewReader("σ.0=one\nς.1=two\nσ.1=three"), false)
		require.ErrorIs(t, err, ErrConflictingKey)

		require.Equal(t, map[string]string{"APP__LOG_LEVEL": "last"}, env.FlattenConfig(data, ""))
		require.Contains(t, string(env.Render(data, nil, false)), "\nAPP__LOG_LEVEL=last\n")
		_, err = (&Markdown{}).Render(t.Context(), data)
		require.ErrorIs(t, err, ErrConflictingKey)
	}
}
