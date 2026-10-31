// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/internal/conversion"
)

func TestUncommentYAMLWithSpecialValues(t *testing.T) {
	t.Parallel()
	conv := NewConfigToEnvConverter()

	input := `# Logger config
#logger:
#  level: error
#  maskString: ****
#  appGroupName: app`

	uncommented := conv.UncommentYAML([]byte(input))
	result := string(uncommented)

	require.Contains(t, result, `maskString: "****"`)
	require.Contains(t, result, `level: error`)
	require.Contains(t, result, `appGroupName: app`)
}

func TestLoadConfigFileAutoUncommentsTemplates(t *testing.T) {
	t.Parallel()
	// Create a temporary fully-commented YAML file
	dir := t.TempDir()
	content := `# Config template
#server:
#  host: localhost
#  port: 8080
#  timeout: 30s
`
	filePath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	cmd := &Command{fromPath: dir}
	data, err := cmd.loadConfigFile(filePath, formatYAML, false)
	require.NoError(t, err)

	require.NotEmpty(t, data)
	serverData, ok := data["server"].(map[string]any)
	require.True(t, ok, "expected 'server' key with nested map")
	require.Equal(t, "localhost", serverData["host"])
	require.Equal(t, 8080, serverData["port"])
	require.Equal(t, "30s", serverData["timeout"])
}

func TestLoadConfigFilePreservesRealContent(t *testing.T) {
	t.Parallel()
	// Create a temporary YAML with real (uncommented) content
	dir := t.TempDir()
	content := `server:
  host: production.example.com
  port: 9090
# commented:
#   key: value
`
	filePath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	cmd := &Command{fromPath: dir}
	data, err := cmd.loadConfigFile(filePath, formatYAML, false)
	require.NoError(t, err)

	// Only the uncommented content should be present
	serverData, ok := data["server"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "production.example.com", serverData["host"])
	require.Equal(t, 9090, serverData["port"])
	// Commented-out "commented" key should NOT be present
	_, hasCommented := data["commented"]
	require.False(t, hasCommented, "commented key should not be auto-uncommented in mixed files")
}

func TestLoadAndMergeParallelReturnsErrorOnEmpty(t *testing.T) {
	t.Parallel()
	// Create a temp directory with enough empty YAML files to trigger parallel processing
	dir := t.TempDir()
	const fileCount = 6
	for i := range fileCount {
		name := filepath.Join(dir, "config"+string(rune('a'+i))+".yaml")
		require.NoError(t, os.WriteFile(name, []byte("# just a comment\n"), 0o644))
	}

	cmd := &Command{fromPath: dir}
	_, err := cmd.loadAndMergeDirectory(t.Context(), formatYAML, false)
	require.Error(t, err)
	require.ErrorIs(t, err, conversion.ErrEmptyInput)
}

func TestConvertDataWithCommentsTemplateYAML(t *testing.T) {
	t.Parallel()
	conv := NewConfigToEnvConverter()
	data := map[string]any{
		"logger": map[string]any{
			"level":      "error",
			"maskString": "****",
		},
	}

	outPath := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, conv.ConvertData(data, outPath))

	content, err := os.ReadFile(outPath)
	require.NoError(t, err)

	output := string(content)
	require.Contains(t, output, "LOGGER__LEVEL=error")
	require.Contains(t, output, "LOGGER__MASK_STRING=****")
}

func TestFullEndToEndDirectoryConversion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create multiple fully-commented template files
	file1 := `# Server configuration template
#server:
#  host: 0.0.0.0
#  port: 8080
`
	file2 := `# Database configuration template
#database:
#  host: localhost
#  port: 5432
#  name: mydb
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "server.yaml"), []byte(file1), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "database.yaml"), []byte(file2), 0o644))

	outPath := filepath.Join(t.TempDir(), ".env")
	cmd := &Command{
		fromPath: dir,
		toPath:   outPath,
	}

	// Simulate the directory conversion flow
	mergedData, err := cmd.loadAndMergeDirectory(t.Context(), formatYAML, false)
	require.NoError(t, err)

	conv := NewConfigToEnvConverter()
	require.NoError(t, conv.ConvertData(mergedData, outPath))

	content, err := os.ReadFile(outPath)
	require.NoError(t, err)

	output := string(content)
	require.True(t, len(output) > 0, "output should not be empty")

	// Nested objects are sections named in SCREAMING_SNAKE_CASE.
	require.Contains(t, output, "DATABASE__HOST=localhost")
	require.Contains(t, output, "DATABASE__PORT=5432")
	require.Contains(t, output, "DATABASE__NAME=mydb")
	require.Contains(t, output, "SERVER__HOST=0.0.0.0")
	require.Contains(t, output, "SERVER__PORT=8080")
}
