// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/internal/conversion"
)

func FuzzParse(f *testing.F) {
	for _, s := range []string{"A=1", "A__0=x", "A__999999999=x", "A=x\nA__B=y", "A='literal'"} {
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
			require.Equal(t, (&conversion.Env{}).Render(a, nil, false), (&conversion.Env{}).Render(b, nil, false))
		}
	})
}
