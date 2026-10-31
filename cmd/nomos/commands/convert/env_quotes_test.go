// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

func TestEnvConversionKeepsLiteralQuotes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	input := filepath.Join(dir, "input.env")
	output := filepath.Join(dir, "out.yaml")
	require.NoError(t, os.WriteFile(input, []byte("ONE=secret'\nTWO=\"'quoted'\"\nTHREE=\"unfinished\n"), 0o600))
	require.NoError(t, convert.NewEnvToConfigConverter().Convert(input, output, "yaml"))
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, (&yaml3.Backend{}).Decode(bytes.NewReader(data), &got))
	require.Equal(t, "secret'", got["one"])
	require.Equal(t, "'quoted'", got["two"])
	require.Equal(t, "\"unfinished", got["three"])
}
