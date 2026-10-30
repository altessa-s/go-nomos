// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPackageBoundaries(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	packages := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && path != root || entry.Name() == "devtools" || entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		packages[dir] = true
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if imp == "gopkg.in/yaml.v3" && dir != "loader/backend/yaml3" {
				t.Errorf("%s imports YAML outside its adapter", rel)
			}
			if imp == "github.com/BurntSushi/toml" && dir != "loader/backend/toml" {
				t.Errorf("%s imports TOML outside its adapter", rel)
			}
			if strings.HasPrefix(dir, "internal/") && strings.Contains(imp, "go-nomos/cmd/") {
				t.Errorf("%s depends on CLI", rel)
			}
			if dir == "internal/conversion" && (imp == "os" || imp == "net/http" || strings.HasPrefix(imp, "github.com/altessa-s/go-atlas-net/") || strings.Contains(imp, "/cobra") || strings.Contains(imp, "go-nomos/loader/backend")) {
				t.Errorf("%s couples conversion to adapter %s", rel, imp)
			}
			// Black-box tests (package loader_test) are loader clients and may pass a concrete
			// backend to loader.New; white-box tests and production files may not.
			if dir == "loader" && rel != "loader/new.go" && file.Name.Name != "loader_test" && strings.Contains(imp, "go-nomos/loader/backend/") {
				t.Errorf("%s couples loader operations to concrete backend", rel)
			}
		}
		return nil
	})
	require.NoError(t, err)
	for dir := range packages {
		for _, name := range []string{"doc.go", "README.md"} {
			_, err := os.Stat(filepath.Join(root, dir, name))
			require.NoError(t, err, "%s needs %s", dir, name)
		}
	}
}
