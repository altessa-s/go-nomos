// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion_test

import (
	"maps"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/internal/conversion"
)

func FuzzParse(f *testing.F) {
	for _, s := range []string{"A=1", "A=[NaN,[NaN]]", "A__0=x", "A__999999999=x", "A=x\nA__B=y", "A='literal'"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		t.Parallel()

		if len(s) > 8192 {
			return
		}

		p := &conversion.Parser{}
		a, e1 := p.Parse(strings.NewReader(s), false)
		b, e2 := p.Parse(strings.NewReader(s), false)
		require.Equal(t, e1 == nil, e2 == nil)

		if e1 == nil {
			require.True(t, maps.EqualFunc(a, b, equalParsedValues), "parsed maps differ: %#v and %#v", a, b)
			require.Equal(t, (&conversion.Env{}).Render(a, nil, false), (&conversion.Env{}).Render(b, nil, false))

			md := &conversion.Markdown{}
			first, err := md.Render(t.Context(), a)
			require.NoError(t, err)
			second, err := md.Render(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, first, second)
		}
	})
}

// Compare floating-point bits so parsed NaNs and signed zero are checked exactly.
func equalParsedValues(a, b any) bool {
	switch v := a.(type) {
	case map[string]any:
		w, ok := b.(map[string]any)
		return ok && maps.EqualFunc(v, w, equalParsedValues)
	case []any:
		w, ok := b.([]any)
		return ok && slices.EqualFunc(v, w, equalParsedValues)
	case float64:
		w, ok := b.(float64)
		return ok && math.Float64bits(v) == math.Float64bits(w)
	default:
		return a == b
	}
}
