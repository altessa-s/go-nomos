//go:build unix

// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A file accepted during discovery and then replaced by a symlink leading
// outside the configured directory (or whose parent directory is) is not
// read: the read goes through the directory's os.Root.
func TestReadContent_RefusesPathSwappedAfterDiscovery(t *testing.T) {
	t.Parallel()
	outside := filepath.Join(t.TempDir(), "secret.yaml")
	if err := os.WriteFile(outside, []byte("appName: leaked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	accepted := filepath.Join(dir, "extra.yaml")
	if err := os.WriteFile(accepted, []byte("appName: accepted\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cf := newTestLoadState(t, WithPath(dir))
	if err := cf.loadFiles(); err != nil {
		t.Fatalf("loadFiles: %v", err)
	}
	t.Cleanup(func() { _ = cf.root.Close() })

	if err := os.Remove(accepted); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, accepted); err != nil {
		t.Fatal(err)
	}
	for f := range cf.files.All() {
		err := cf.readContent(f)
		if err == nil {
			t.Fatalf("read %s through a swapped symlink: %q", f.name, f.content)
		}
		if strings.Contains(f.content, "leaked") {
			t.Fatalf("content from outside the root was read: %q", f.content)
		}
	}
}

// Replacing the configured root's path after discovery, before the first
// include is read, cannot redirect an include outside the original root, for
// a configured directory and for a single configured file.
func TestReadContent_IncludeRefusesSwappedRoot(t *testing.T) {
	t.Parallel()
	for _, single := range []bool{false, true} {
		name := "directory"
		if single {
			name = "single file"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			outside := t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "extra.yaml"), []byte("appName: leaked\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			// A single configured file is read by its path, so the outside
			// directory has one too: only its include is checked here.
			if err := os.WriteFile(filepath.Join(outside, "main.yaml"), []byte("!include extra.yaml\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			parent := t.TempDir()
			dir := filepath.Join(parent, "conf")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			main := filepath.Join(dir, "main.yaml")
			if err := os.WriteFile(main, []byte("!include extra.yaml\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			path := dir
			if single {
				path = main
			}
			cf := newTestLoadState(t, WithPath(path))
			if err := cf.loadFiles(); err != nil {
				t.Fatalf("loadFiles: %v", err)
			}
			t.Cleanup(func() { _ = cf.root.Close() })

			// The original directory moves away; its path now leads outside.
			if err := os.Rename(dir, filepath.Join(parent, "moved")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, dir); err != nil {
				t.Fatal(err)
			}
			for f := range cf.files.All() {
				if f.name != "main.yaml" {
					continue
				}
				err := cf.readContent(f)
				if err == nil {
					t.Fatalf("include read through a swapped root: %q", f.content)
				}
				if strings.Contains(f.content, "leaked") {
					t.Fatalf("content from outside the root was read: %q", f.content)
				}
			}
		})
	}
}

// A config root replaced by a FIFO before it is opened fails at once instead
// of blocking the load.
func TestOpenRoot_RefusesFIFO(t *testing.T) {
	t.Parallel()
	fifo := filepath.Join(t.TempDir(), "conf")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	cf := newTestLoadState(t, WithPath(fifo))
	done := make(chan error, 1)
	go func() { done <- cf.openRoot(fifo) }()
	select {
	case err := <-done:
		if err == nil {
			_ = cf.root.Close()
			t.Fatal("a FIFO was opened as the config root")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opening a FIFO as the config root blocked")
	}
}
