// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/internal/conversion"
)

func TestEnvStringRoundTrip(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", " true ", "true", "001", "1.5", "NaN", "[a,b]", "first\nADMIN=true", "line\r\nnext", " tab\t ", `C:\new\token`, `"quoted"`, "'quoted'", "a,b", "a#b", "$HOME", "`cmd`", "\x00"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			doc := (&conversion.Env{}).Render(map[string]any{"value": value}, nil, false)
			got, err := (&conversion.Parser{}).Parse(bytes.NewReader(doc), false)
			require.NoError(t, err)
			require.Equal(t, map[string]any{"value": value}, got)
			require.NotContains(t, string(doc), "\nADMIN=")
			// FlattenConfig remains a raw-value API.
			require.Equal(t, value, (&conversion.Env{}).FlattenConfig(map[string]any{"value": value}, "")["VALUE"])
		})
	}
}

func TestEnvArrayRoundTrip(t *testing.T) {
	t.Parallel()
	want := map[string]any{"values": []any{"a,b", "true", " a ", "line\nnext", `a"b`, "", int64(12), true, []any{"nested,comma", int64(3)}}}
	doc := (&conversion.Env{}).Render(want, nil, false)
	got, err := (&conversion.Parser{}).Parse(bytes.NewReader(doc), false)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestEnvQuotedTypes(t *testing.T) {
	t.Parallel()
	got, err := (&conversion.Parser{}).Parse(strings.NewReader("A=\"true\"\nB='123'\nC=true\nD=123\n"), false)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"a": "true", "b": "123", "c": true, "d": int64(123)}, got)
}

func FuzzEnvStringRoundTrip(f *testing.F) {
	for _, value := range []string{"normal", " first\nADMIN=true ", "true", "\x00", `\"`} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		t.Parallel()
		if len(value) > 4096 {
			t.Skip()
		}
		doc := (&conversion.Env{}).Render(map[string]any{"value": value}, nil, false)
		got, err := (&conversion.Parser{}).Parse(bytes.NewReader(doc), false)
		require.NoError(t, err)
		require.Equal(t, map[string]any{"value": value}, got)
	})
}

func TestEnvCommentedRoundTrip(t *testing.T) {
	t.Parallel()
	data := map[string]any{"value": "first\nADMIN=true"}
	doc := (&conversion.Env{}).Render(data, map[string]bool{"VALUE": true}, true)
	got, err := (&conversion.Parser{}).Parse(bytes.NewReader(doc), true)
	require.NoError(t, err)
	require.Equal(t, data, got)
	ignored, err := (&conversion.Parser{}).Parse(bytes.NewReader(doc), false)
	require.NoError(t, err)
	require.Empty(t, ignored)
}

func TestEnvCamelCaseSiblingSections(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"fileStore": map[string]any{"dir": "/data"},
		"tlsConfig": map[string]any{"enabled": true},
		"grpc": map[string]any{
			"realIP":    map[string]any{"enabled": true},
			"rateLimit": map[string]any{"maxRequests": 10},
		},
	}
	require.Equal(t, map[string]string{
		"FILE_STORE__DIR":                "/data",
		"TLS_CONFIG__ENABLED":            "true",
		"GRPC__REAL_IP__ENABLED":         "true",
		"GRPC__RATE_LIMIT__MAX_REQUESTS": "10",
	}, (&conversion.Env{}).FlattenConfig(data, ""))
}

func TestEnvExcessiveArrayNestingStaysLiteral(t *testing.T) {
	t.Parallel()
	value := strings.Repeat("[", 1000) + "x" + strings.Repeat("]", 1000)
	got, err := (&conversion.Parser{}).Parse(strings.NewReader("VALUE="+value), false)
	require.NoError(t, err)
	require.Equal(t, value, got["value"])
}

func TestEnvRejectsInvalidUTF8Names(t *testing.T) {
	t.Parallel()
	_, err := (&conversion.Parser{}).Parse(strings.NewReader("\xba=8\n\x8b=1"), false)
	require.ErrorIs(t, err, conversion.ErrInvalidInput)
}
