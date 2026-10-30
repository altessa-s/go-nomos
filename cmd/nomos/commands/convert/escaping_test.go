// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

func TestUncommentYAMLPreservesBackslashes(t *testing.T) {
	t.Parallel()
	for _, value := range []string{`*\new\token`, `@C:\keys\private`, `!\"quoted"`} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			data := convert.NewConfigToEnvConverter().UncommentYAML([]byte("# value: " + value))
			var got map[string]string
			require.NoError(t, (&yaml3.Backend{}).Decode(bytes.NewReader(data), &got))
			require.Equal(t, value, got["value"])
		})
	}
}
