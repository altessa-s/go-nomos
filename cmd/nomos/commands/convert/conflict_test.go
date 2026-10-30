// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
)

func TestEnvToConfigRejectsConflictingPaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, input string }{
		{"scalar and object", "APP=scalar\nAPP__HOST=localhost\n"},
		{"scalar and array", "APP=scalar\nAPP__0=value\n"},
		{"object and array", "APP__HOST=localhost\nAPP__0=value\n"},
		{"array element and field", "APP__0=value\nAPP__0__HOST=localhost\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			input := filepath.Join(dir, "input.env")
			require.NoError(t, os.WriteFile(input, []byte(tc.input), 0o600))
			err := convert.NewEnvToConfigConverter().Convert(input, filepath.Join(dir, "output.yaml"), "yaml")
			require.ErrorIs(t, err, convert.ErrConflictingKey)
		})
	}
}
