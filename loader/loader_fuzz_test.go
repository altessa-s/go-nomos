// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader"
)

func FuzzLoad_EnvParsing(f *testing.F) {
	f.Add("NAME", "plain")
	f.Add("PORT", "42")
	f.Add("PORT", "invalid")
	f.Add("ITEMS__0", "x")
	f.Fuzz(func(t *testing.T, key, value string) {
		// Environment mutation is process-global; this target must remain serial.
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) {
			return
		}
		if len(key) > 1024 || len(value) > 4096 {
			return
		}
		t.Setenv("NOMOSFUZZ_"+key, value)
		type schema struct {
			Name  string
			Port  int
			Items []string
		}
		a, b := &schema{}, &schema{}
		opts := []loader.Option{loader.WithEnvPrefix("NOMOSFUZZ"), loader.WithMaxEnvSliceLen(32)}
		_, e1 := loader.New(nil, opts...).LoadContext(t.Context(), a)
		_, e2 := loader.New(nil, opts...).LoadContext(t.Context(), b)
		require.Equal(t, e1 == nil, e2 == nil)
		if e1 == nil {
			require.Equal(t, a, b)
		}
		if key == "PORT" && !strings.Contains(value, "$") {
			if n, err := strconv.Atoi(value); err == nil {
				require.NoError(t, e1)
				require.Equal(t, n, a.Port)
			}
		}
	})
}
