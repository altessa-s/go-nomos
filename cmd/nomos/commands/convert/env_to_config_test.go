// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

func TestEnvToConfig_Convert(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	outPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(envPath, []byte("SERVERS__0__HOST=localhost\nPORTS__0=8080\n"), 0o600))

	require.NoError(t, NewEnvToConfigConverter().Convert(envPath, outPath, "yaml"))

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, (&yaml3.Backend{}).Decode(bytes.NewReader(data), &got))
	require.Equal(t, map[string]any{
		"servers": []any{map[string]any{"host": "localhost"}},
		"ports":   []any{8080},
	}, got)
}
