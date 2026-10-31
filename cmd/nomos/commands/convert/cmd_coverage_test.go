// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
	"github.com/altessa-s/go-nomos/internal/conversion"
	"github.com/altessa-s/go-nomos/loader/backend/toml"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

const sampleYAML = `server:
  port: 8080
  hosts:
    - a
    - b
  debug: true
  ratio: 0.5
  empty: []
  nothing:
log:
  level: info
`

func writeFile(t testing.TB, path, content string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func readFile(t testing.TB, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func runConvert(t testing.TB, args ...string) (string, error) {
	t.Helper()
	cmd := convert.New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return out.String(), err
}

func TestConvert_YAMLToEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	from := writeFile(t, filepath.Join(dir, "config.yaml"), sampleYAML)
	to := filepath.Join(dir, "out.env")

	out, err := runConvert(t, "--from", from, "--to", to)
	require.NoError(t, err)
	require.Contains(t, out, "(yaml) -> "+to+" (env)")

	env := readFile(t, to)
	require.Contains(t, env, "# Log Configuration\n")
	require.Contains(t, env, "# Server Configuration\n")
	require.Contains(t, env, "SERVER__PORT=8080\n")
	require.Contains(t, env, "SERVER__HOSTS=[a,b]\n")
	require.Contains(t, env, "SERVER__EMPTY=[]\n")
	require.Contains(t, env, "SERVER__DEBUG=true\n")
	require.Contains(t, env, "LOG__LEVEL=info\n")
	require.NotContains(t, env, "SERVER__NOTHING", "nil values are skipped")
}

func TestConvert_YAMLToEnvParseComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	t.Run("inline comments mark keys", func(t *testing.T) {
		t.Parallel()
		from := writeFile(t, filepath.Join(dir, "inline.yaml"), "app:\n  name: demo # optional\n  port: 80\n")
		to := filepath.Join(dir, "inline.env")
		_, err := runConvert(t, "--from", from, "--to", to, "--parse-comments")
		require.NoError(t, err)
		env := readFile(t, to)
		require.Contains(t, env, "# APP__NAME=demo\n")
		require.Contains(t, env, "\nAPP__PORT=80\n")
	})

	t.Run("fully commented template marks everything", func(t *testing.T) {
		t.Parallel()
		from := writeFile(t, filepath.Join(dir, "tmpl.yaml"), "# Database settings\n#db:\n#  host: localhost\n#  port: 5432\n")
		to := filepath.Join(dir, "tmpl.env")
		_, err := runConvert(t, "--from", from, "--to", to, "--parse-comments")
		require.NoError(t, err)
		env := readFile(t, to)
		require.Contains(t, env, "# DB__HOST=localhost\n")
		require.Contains(t, env, "# DB__PORT=5432\n")
	})
}

func TestConvert_TOMLToEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	from := writeFile(t, filepath.Join(dir, "c.toml"), "[db]\nhost = \"h\"\nports = [1, 2]\n")
	to := filepath.Join(dir, "c.env")

	_, err := runConvert(t, "--from", from, "--to", to)
	require.NoError(t, err)
	env := readFile(t, to)
	require.Contains(t, env, "DB__HOST=h\n")
	require.Contains(t, env, "DB__PORTS=[1,2]\n")
}

func TestConvert_EnvToConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	from := writeFile(t, filepath.Join(dir, "in.env"), `
# a prose comment line without assignment is skipped when comments are off
SERVER__PORT=8080
SERVER__HOSTS=[a, b ,3]
SERVER__EMPTY=[]
SERVER__NAME="quoted"
#SERVER__DISABLED=true
`)

	t.Run("yaml", func(t *testing.T) {
		t.Parallel()
		to := filepath.Join(dir, "out.yaml")
		_, err := runConvert(t, "--from", from, "--to", to)
		require.NoError(t, err)

		var got map[string]any
		require.NoError(t, (&yaml3.Backend{}).Decode(bytes.NewReader([]byte(readFile(t, to))), &got))
		server := got["server"].(map[string]any)
		require.Equal(t, 8080, server["port"])
		require.Equal(t, []any{"a", "b", 3}, server["hosts"])
		require.Equal(t, []any{}, server["empty"])
		require.Equal(t, "quoted", server["name"])
		require.NotContains(t, server, "disabled")
	})

	t.Run("toml with comments", func(t *testing.T) {
		t.Parallel()
		src := writeFile(t, filepath.Join(dir, "c.env"), "#\n# A__ON=true\nA__RATIO=0.25\n")
		to := filepath.Join(dir, "out.toml")
		_, err := runConvert(t, "--from", src, "--to", to, "--parse-comments")
		require.NoError(t, err)

		var got map[string]any
		err = (&toml.Backend{}).Decode(strings.NewReader(readFile(t, to)), &got)
		require.NoError(t, err)
		a := got["a"].(map[string]any)
		require.Equal(t, true, a["on"])
		require.InDelta(t, 0.25, a["ratio"], 1e-9)
	})

	t.Run("invalid line", func(t *testing.T) {
		t.Parallel()
		src := writeFile(t, filepath.Join(dir, "bad.env"), "OK=1\nnot a pair\n")
		_, err := runConvert(t, "--from", src, "--to", filepath.Join(dir, "bad.yaml"))
		require.ErrorIs(t, err, conversion.ErrInvalidInput)
	})
}

func TestConvert_ConfigToConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	yamlPath := writeFile(t, filepath.Join(dir, "a.yaml"), "svc:\n  name: x\n  port: 1\n")
	tomlPath := filepath.Join(dir, "a.toml")

	_, err := runConvert(t, "--from", yamlPath, "--to", tomlPath)
	require.NoError(t, err)
	var fromTOML map[string]any
	err = (&toml.Backend{}).Decode(strings.NewReader(readFile(t, tomlPath)), &fromTOML)
	require.NoError(t, err)
	require.Equal(t, "x", fromTOML["svc"].(map[string]any)["name"])

	back := filepath.Join(dir, "b.yml")
	_, err = runConvert(t, "--from", tomlPath, "--to", back)
	require.NoError(t, err)
	var roundTrip map[string]any
	require.NoError(t, (&yaml3.Backend{}).Decode(bytes.NewReader([]byte(readFile(t, back))), &roundTrip))
	require.Equal(t, "x", roundTrip["svc"].(map[string]any)["name"])
	require.Equal(t, 1, roundTrip["svc"].(map[string]any)["port"])

	t.Run("explicit formats on unknown extensions", func(t *testing.T) {
		t.Parallel()
		src := writeFile(t, filepath.Join(dir, "in.txt"), "k: v\n")
		dst := filepath.Join(dir, "out.txt")
		_, err := runConvert(t, "--from", src, "--to", dst, "--from-format", "yaml", "--to-format", "toml")
		require.NoError(t, err)
		require.Contains(t, readFile(t, dst), `k = "v"`)
	})

	t.Run("invalid source yaml", func(t *testing.T) {
		t.Parallel()
		src := writeFile(t, filepath.Join(dir, "bad.yaml"), "a: [1\n")
		_, err := runConvert(t, "--from", src, "--to", filepath.Join(dir, "bad-out.toml"))
		require.Error(t, err)
	})

	t.Run("invalid source toml", func(t *testing.T) {
		t.Parallel()
		src := writeFile(t, filepath.Join(dir, "bad.toml"), "a = = 1\n")
		_, err := runConvert(t, "--from", src, "--to", filepath.Join(dir, "bad-out.yml"))
		require.Error(t, err)
	})
}

func TestConvert_ToMarkdown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	t.Run("yaml", func(t *testing.T) {
		t.Parallel()
		from := writeFile(t, filepath.Join(dir, "c.yaml"), sampleYAML+"retry:\n  maxAttempts: 3\n  floats: [1.5]\n  bools: [true]\n")
		to := filepath.Join(dir, "c.md")
		_, err := runConvert(t, "--from", from, "--to", to)
		require.NoError(t, err)

		md := readFile(t, to)
		require.Contains(t, md, "# Environment Variables Reference\n")
		require.Contains(t, md, "- [Server Configuration](#server-configuration)\n")
		require.Contains(t, md, "| Environment Variable | Type | Default Value |\n")
		require.Contains(t, md, "| `SERVER__PORT` | int | `8080` |\n")
		require.Regexp(t, "\\| `SERVER__HOSTS` \\| [^|]+ \\| `\\[a,b\\]` \\|\n", md)
		require.Contains(t, md, "| `SERVER__DEBUG` | bool | `true` |\n")
		require.Contains(t, md, "| `SERVER__RATIO` | float | `0.5` |\n")
		require.Contains(t, md, "| `SERVER__EMPTY` | array | `[]` |\n")
		require.Contains(t, md, "| `SERVER__NOTHING` | string |   |\n")
		require.Contains(t, md, "| `LOG__LEVEL` | string | `info` |\n")
		require.Regexp(t, "\\| `RETRY__FLOATS` \\| [^|]+ \\| `\\[1.5\\]` \\|\n", md)
		require.Regexp(t, "\\| `RETRY__BOOLS` \\| [^|]+ \\| `\\[true\\]` \\|\n", md)
		require.Contains(t, md, "| `RETRY__MAX_ATTEMPTS` | int | `3` |\n")
	})

	t.Run("toml", func(t *testing.T) {
		t.Parallel()
		from := writeFile(t, filepath.Join(dir, "c.toml"), "[db]\nhost = \"h\"\nids = [1, 2]\n")
		to := filepath.Join(dir, "toml.md")
		_, err := runConvert(t, "--from", from, "--to", to)
		require.NoError(t, err)
		md := readFile(t, to)
		require.Contains(t, md, "| `DB__HOST` | string | `h` |\n")
		require.Regexp(t, "\\| `DB__IDS` \\| [^|]+ \\| `\\[1,2\\]` \\|\n", md)
	})
}

func TestConvert_Directory(t *testing.T) {
	t.Parallel()

	t.Run("sequential merge to env", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		writeFile(t, filepath.Join(src, "a.yaml"), "app:\n  name: a\n  nested:\n    x: 1\n")
		writeFile(t, filepath.Join(src, "b.yml"), "app:\n  nested:\n    y: 2\n  name: b\n")
		writeFile(t, filepath.Join(src, "ignored.txt"), "junk")
		to := filepath.Join(t.TempDir(), "out.env")

		out, err := runConvert(t, "--from", src, "--to", to)
		require.NoError(t, err)
		require.Contains(t, out, "(directory) (yaml)")
		env := readFile(t, to)
		require.Contains(t, env, "APP__NESTED__X=1\n")
		require.Contains(t, env, "APP__NESTED__Y=2\n")
		require.Contains(t, env, "APP__NAME=")
	})

	t.Run("parse comments marks all keys", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		writeFile(t, filepath.Join(src, "a.yaml"), "app:\n  name: a\n")
		to := filepath.Join(t.TempDir(), "out.env")

		_, err := runConvert(t, "--from", src, "--to", to, "--parse-comments")
		require.NoError(t, err)
		require.Contains(t, readFile(t, to), "# APP__NAME=a\n")
	})

	t.Run("parallel merge to yaml", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		for i := range 6 {
			writeFile(t, filepath.Join(src, fmt.Sprintf("f%d.yaml", i)), fmt.Sprintf("k%d: %d\n", i, i))
		}
		to := filepath.Join(t.TempDir(), "out.yaml")

		_, err := runConvert(t, "--from", src, "--to", to)
		require.NoError(t, err)
		var got map[string]any
		require.NoError(t, (&yaml3.Backend{}).Decode(bytes.NewReader([]byte(readFile(t, to))), &got))
		for i := range 6 {
			require.Equal(t, i, got[fmt.Sprintf("k%d", i)])
		}
	})

	t.Run("parallel merge with a broken file fails", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		for i := range 5 {
			writeFile(t, filepath.Join(src, fmt.Sprintf("f%d.toml", i)), fmt.Sprintf("k%d = %d\n", i, i))
		}
		writeFile(t, filepath.Join(src, "zz.toml"), "= broken")
		_, err := runConvert(t, "--from", src, "--to", filepath.Join(t.TempDir(), "o.yaml"))
		require.Error(t, err)
	})

	t.Run("commented template is auto-uncommented to markdown", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		writeFile(t, filepath.Join(src, "tmpl.yaml"), "#cache:\n#  ttl: 5\n")
		to := filepath.Join(t.TempDir(), "out.md")

		_, err := runConvert(t, "--from", src, "--to", to)
		require.NoError(t, err)
		require.Contains(t, readFile(t, to), "| `CACHE__TTL` | int | `5` |\n")
	})

	t.Run("commented template auto-detected without parse-comments", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		writeFile(t, filepath.Join(src, "tmpl.yaml"), "#cache:\n#  ttl: 5\n")
		to := filepath.Join(t.TempDir(), "out.env")

		_, err := runConvert(t, "--from", src, "--to", to)
		require.NoError(t, err)
		require.Contains(t, readFile(t, to), "CACHE__TTL=5\n")
	})

	t.Run("toml directory to toml", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		writeFile(t, filepath.Join(src, "a.toml"), "a = 1\n")
		to := filepath.Join(t.TempDir(), "out.toml")
		_, err := runConvert(t, "--from", src, "--to", to)
		require.NoError(t, err)
		require.Contains(t, readFile(t, to), "a = 1")
	})

	t.Run("env directory rejected", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		writeFile(t, filepath.Join(src, "a.env"), "A=1\n")
		for _, target := range []string{"o.yaml", "o.toml", "o.md"} {
			to := filepath.Join(t.TempDir(), target)
			_, err := runConvert(t, "--from", src, "--to", to)
			require.ErrorIs(t, err, convert.ErrEnvDirectoryInput, target)
			require.NoFileExists(t, to)
		}
	})

	t.Run("no config files", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		writeFile(t, filepath.Join(src, "readme.txt"), "x")
		_, err := runConvert(t, "--from", src, "--to", filepath.Join(t.TempDir(), "o.env"))
		require.Error(t, err)
	})

	t.Run("matching files but empty content", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		writeFile(t, filepath.Join(src, "a.yaml"), "")
		_, err := runConvert(t, "--from", src, "--to", filepath.Join(t.TempDir(), "o.env"), "--from-format", "yaml")
		require.Error(t, err)
	})

	t.Run("broken file in sequential merge", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		writeFile(t, filepath.Join(src, "a.yaml"), "a: [1\n")
		_, err := runConvert(t, "--from", src, "--to", filepath.Join(t.TempDir(), "o.env"))
		require.Error(t, err)
	})
}

func TestConvert_Errors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	yamlPath := writeFile(t, filepath.Join(dir, "c.yaml"), "a: 1\n")
	mdPath := writeFile(t, filepath.Join(dir, "c.md"), "# doc\n")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing source", []string{"--from", filepath.Join(dir, "nope.yaml"), "--to", filepath.Join(dir, "o.env")}, "source path does not exist"},
		{"missing target dir", []string{"--from", yamlPath, "--to", filepath.Join(dir, "no", "o.env")}, "target directory does not exist"},
		{"undetectable target", []string{"--from", yamlPath, "--to", filepath.Join(dir, "o.out")}, "to format could not be detected"},
		{"unsupported source format", []string{"--from", yamlPath, "--to", filepath.Join(dir, "o.env"), "--from-format", "xml"}, "unsupported from format: xml"},
		{"unsupported pair", []string{"--from", mdPath, "--to", filepath.Join(dir, "o.yaml")}, "unsupported conversion: md to yaml"},
		{"missing required flags", []string{"--from", yamlPath}, `required flag(s) "to" not set`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := runConvert(t, tc.args...)
			require.Error(t, err)
		})
	}
}

// TestConvert_ClaudeKeyRequired is serial: it clears ANTHROPIC_API_KEY via t.Setenv.
func TestConvert_ClaudeKeyRequired(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	dir := t.TempDir()
	from := writeFile(t, filepath.Join(dir, "c.yaml"), "a: 1\n")
	_, err := runConvert(t, "--from", from, "--to", filepath.Join(dir, "c.md"), "--use-claude")
	require.Error(t, err)
}

func TestConvert_ToMarkdownArrayElementTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, file, content string
		want                []string
	}{
		{
			name: "yaml", file: "a.yaml",
			content: "app:\n  hosts: [a, b]\n  ports: [80, 443]\n  ratios: [0.5, 1.5]\n  flags: [true, false]\n" +
				"  matrix: [[1, 2], [3]]\n  mixed: [a, 1]\n  servers: [{host: h}]\n  empty: []\n",
			want: []string{
				"| `APP__HOSTS` | []string |", "| `APP__PORTS` | []int |", "| `APP__RATIOS` | []float |",
				"| `APP__FLAGS` | []bool |", "| `APP__MATRIX` | [][]int |", "| `APP__MIXED` | array |",
				"| `APP__SERVERS` | []map |", "| `APP__EMPTY` | array |",
			},
		},
		{
			name: "toml", file: "a.toml",
			content: "[app]\nhosts = [\"a\", \"b\"]\nports = [80, 443]\nmatrix = [[1, 2], [3]]\n",
			want:    []string{"| `APP__HOSTS` | []string |", "| `APP__PORTS` | []int |", "| `APP__MATRIX` | [][]int |"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			from := writeFile(t, filepath.Join(dir, tc.file), tc.content)
			to := filepath.Join(dir, "out.md")
			_, err := runConvert(t, "--from", from, "--to", to)
			require.NoError(t, err)
			md := readFile(t, to)
			for _, w := range tc.want {
				require.Contains(t, md, w)
			}
		})
	}
}
