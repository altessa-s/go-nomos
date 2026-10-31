// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

func FuzzTemplateSpecialValues(f *testing.F) {
	f.Add("secret")
	f.Add(`path\to\file`)
	f.Add(`quoted"value`)
	f.Fuzz(func(t *testing.T, value string) {
		t.Parallel()
		if len(value) > 4096 || strings.ContainsAny(value, "\r\n") || !utf8.ValidString(value) {
			return
		}
		original := []byte("#key: *" + value)
		before := bytes.Clone(original)
		b := &yaml3.Backend{}
		out := b.Uncomment(original)
		require.Equal(t, before, original)
		require.Equal(t, out, b.Uncomment(original))
		var got map[string]string
		require.NoError(t, b.Decode(bytes.NewReader(out), &got))
		require.Equal(t, "*"+value, got["key"])
	})
}
