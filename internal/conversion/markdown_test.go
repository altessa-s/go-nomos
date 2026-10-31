// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/internal/conversion"
)

type describeFunc func(context.Context, map[string]string, string) (map[string]string, error)

func (f describeFunc) Describe(ctx context.Context, vars map[string]string, section string) (map[string]string, error) {
	return f(ctx, vars, section)
}
func TestMarkdownDescriptionPort(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("provider failed")
	provider := describeFunc(func(ctx context.Context, vars map[string]string, section string) (map[string]string, error) {
		require.Equal(t, "PORT", section)
		require.Contains(t, vars, "PORT")
		return nil, sentinel
	})
	_, err := (&conversion.Markdown{}).Render(t.Context(), map[string]any{"port": 8080}, provider)
	require.ErrorIs(t, err, sentinel)
}

func TestMarkdownValuesStayLocal(t *testing.T) {
	t.Parallel()
	const secret = "synthetic-secret-value"
	provider := describeFunc(func(ctx context.Context, vars map[string]string, section string) (map[string]string, error) {
		require.Equal(t, map[string]string{"PASSWORD": "type: string"}, vars)
		return map[string]string{"PASSWORD": "Password"}, nil
	})
	doc, err := (&conversion.Markdown{}).Render(t.Context(), map[string]any{"password": secret}, provider)
	require.NoError(t, err)
	require.Contains(t, string(doc), secret)
}

func TestMarkdownEscapesCells(t *testing.T) {
	t.Parallel()
	provider := describeFunc(func(ctx context.Context, vars map[string]string, section string) (map[string]string, error) {
		return map[string]string{"VALUE": "text|column\r\n`code`<script>"}, nil
	})
	for _, withProvider := range []bool{false, true} {
		t.Run(fmt.Sprint(withProvider), func(t *testing.T) {
			t.Parallel()
			var describer conversion.Describer
			if withProvider {
				describer = provider
			}
			doc, err := (&conversion.Markdown{}).Render(t.Context(), map[string]any{"value": "a|b\n`code`<script>"}, describer)
			require.NoError(t, err)
			require.Contains(t, string(doc), "<code>a&#124;b<br>&#96;code&#96;&lt;script&gt;</code>")
			for line := range strings.SplitSeq(string(doc), "\n") {
				if strings.HasPrefix(line, "| `VALUE`") {
					expected := 4
					if withProvider {
						expected = 5
					}
					require.Equal(t, expected, strings.Count(line, "|"))
				}
			}
			if withProvider {
				require.Contains(t, string(doc), "text&#124;column<br>&#96;code&#96;&lt;script&gt;")
			}
		})
	}
}

func TestMarkdownTypeDetection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"seconds", "30s", "duration"},
		{"compound", "1h30m", "duration"},
		{"word ending in s", "yes", "string"},
		{"plural word", "items", "string"},
		{"numeric string", "5", "string"},
		{"zero string", "0", "string"},
		{"int", 30, "int"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			provider := describeFunc(func(ctx context.Context, vars map[string]string, section string) (map[string]string, error) {
				require.Equal(t, map[string]string{"VALUE": "type: " + tc.want}, vars)
				return nil, nil
			})
			_, err := (&conversion.Markdown{}).Render(t.Context(), map[string]any{"value": tc.value}, provider)
			require.NoError(t, err)
		})
	}
}

func TestMarkdownValueTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value any
		row   string
	}{
		{"nil", nil, "| `V` | string |   |"},
		{"bool", true, "| `V` | bool | `true` |"},
		{"int8", int8(-3), "| `V` | int | `-3` |"},
		{"int64", int64(1) << 40, "| `V` | int | `1099511627776` |"},
		{"uint", uint(7), "| `V` | uint | `7` |"},
		{"uint64", uint64(9), "| `V` | uint | `9` |"},
		{"float32", float32(1.5), "| `V` | float | `1.5` |"},
		{"float64", 0.25, "| `V` | float | `0.25` |"},
		{"string", "plain", "| `V` | string | `plain` |"},
		{"duration", "250ms", "| `V` | duration | `250ms` |"},
		{"other kind", struct{ A int }{4}, "| `V` | string | `{4}` |"},
		{"empty array", []any{}, "| `V` | array | `[]` |"},
		{"strings", []any{"a", "b"}, "| `V` | []string | `[a,b]` |"},
		{"typed ints", []int{1, 2}, "| `V` | []int | `[1,2]` |"},
		{"go array", [2]int{3, 4}, "| `V` | []int | `[3,4]` |"},
		{"bools", []any{true, false}, "| `V` | []bool | `[true,false]` |"},
		{"uints", []any{uint8(1), uint16(2)}, "| `V` | []uint | `[1,2]` |"},
		{"floats", []any{1.5, 2.5}, "| `V` | []float | `[1.5,2.5]` |"},
		{"maps", []any{map[string]any{"k": 1}}, "| `V` | []map | `[map[k:1]]` |"},
		{"nested", []any{[]any{1, 2}, []any{3}}, "| `V` | [][]int | `[[1 2],[3]]` |"},
		{"nested empty", []any{[]any{}}, "| `V` | array | `[[]]` |"},
		{"mixed", []any{"a", 1}, "| `V` | array | `[a,1]` |"},
		{"nil element", []any{nil}, "| `V` | array | `[]` |"},
		{"nil among elements", []any{"a", nil, "b"}, "| `V` | array | `[a,,b]` |"},
		{"untyped element", []any{struct{}{}}, "| `V` | array | `[{}]` |"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, err := (&conversion.Markdown{}).Render(t.Context(), map[string]any{"v": tc.value}, nil)
			require.NoError(t, err)
			require.Contains(t, strings.Split(string(doc), "\n"), tc.row)
		})
	}
}

func TestMarkdownRenderGolden(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"server": map[string]any{"port": 8080, "timeout": "30s", "tls": map[string]any{"enabled": false}},
		"names":  []any{"x"},
	}
	const want = "# Environment Variables Reference\n\n" +
		"This document describes all available environment variables for configuration.\n\n" +
		"## Table of Contents\n\n" +
		"- [Names Configuration](#names-configuration)\n" +
		"- [Server Configuration](#server-configuration)\n\n" +
		"## Names Configuration\n\n" +
		"| Environment Variable | Type | Default Value |\n" +
		"|---------------------|------|---------------|\n" +
		"| `NAMES` | []string | `[x]` |\n\n" +
		"## Server Configuration\n\n" +
		"| Environment Variable | Type | Default Value |\n" +
		"|---------------------|------|---------------|\n" +
		"| `SERVER__PORT` | int | `8080` |\n" +
		"| `SERVER__TIMEOUT` | duration | `30s` |\n" +
		"| `SERVER__TLS__ENABLED` | bool | `false` |\n\n"
	doc, err := (&conversion.Markdown{}).Render(t.Context(), data, nil)
	require.NoError(t, err)
	require.Equal(t, want, string(doc))
}
