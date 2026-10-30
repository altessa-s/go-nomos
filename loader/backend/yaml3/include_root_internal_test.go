//go:build unix

// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3

import (
	"os"
	"path/filepath"
	"testing"
)

// An include path that passed the checks and was then replaced by a symlink
// leading outside the root is not read: includes open through the root.
func TestReadInclude_RefusesEscapeThroughRoot(t *testing.T) {
	t.Parallel()
	outside := filepath.Join(t.TempDir(), "secret.yaml")
	if err := os.WriteFile(outside, []byte("secret: leaked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extra.yaml"), []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "inner.yaml"), []byte("b: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := &includeRoot{dir: dir, given: dir, owned: true}
	defer root.close()
	if err := root.pin(); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if _, err := readInclude(root, "extra.yaml", 0); err != nil {
		t.Fatalf("accepted include: %v", err)
	}

	// The file itself and a parent directory are swapped after the checks.
	if err := os.Remove(filepath.Join(dir, "extra.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "extra.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(dir, "sub")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"extra.yaml", filepath.Join("sub", "secret.yaml")} {
		if content, err := readInclude(root, rel, 0); err == nil {
			t.Fatalf("read %s through a swapped symlink: %q", rel, content)
		}
	}
}

// Once the root is pinned (before the first include is checked), replacing
// the root's path with a symlink to an outside directory makes the include
// fail the traversal check instead of being read from outside.
func TestProcessIncludes_RefusesRootSwappedAfterPin(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "extra.yaml"), []byte("secret: leaked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "conf")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.yaml"), []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := &includeRoot{dir: dir, given: dir, owned: true}
	defer root.close()
	if err := root.pin(); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if err := os.Rename(dir, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	out, err := processIncludes("!include extra.yaml\n", dir, 0, map[string]bool{}, root, 0)
	if err == nil {
		t.Fatalf("include read through a swapped root: %q", out)
	}
}
