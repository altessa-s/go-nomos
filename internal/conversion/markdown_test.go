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
