// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3

import (
	"bufio"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/altessa-s/go-atlas-core/io/files"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
	coreio "github.com/altessa-s/go-atlas-core/io"
)

const (
	maxIncludeDepth         = 10
	includeDirective        = "!include"
	includeRegexMatchGroups = 3 // full match + indent + path
)

var includeRegex = regexp.MustCompile(`^(\s*)` + regexp.QuoteMeta(includeDirective) + `\s+(.+)$`)

// Preprocess performs YAML-specific pre-processing, such as handling !include directives.
// When maxBytes is positive, every included file and the expanded document are bounded
// by it; exceeding the bound returns an error wrapping [coreio.ErrReadLimitExceeded].
// The root directory is opened before the first include is checked, its
// resolved path pinned, and held for the whole expansion;
// [Backend.PreprocessIn] uses one the caller already holds.
func (b *Backend) Preprocess(content, currentDir, rootDir string, maxBytes int64) (string, error) {
	root := &includeRoot{dir: rootDir, given: rootDir, owned: true}
	defer root.close()
	return processIncludes(content, currentDir, 0, map[string]bool{}, root, maxBytes)
}

// PreprocessIn is [Backend.Preprocess] reading every include through root,
// which the caller holds open (rootDir is its path as resolved when opened):
// replacing the root's path afterwards cannot redirect an include.
func (b *Backend) PreprocessIn(content, currentDir string, root *os.Root, rootDir string, maxBytes int64) (string, error) {
	return processIncludes(content, currentDir, 0, map[string]bool{}, &includeRoot{dir: rootDir, given: rootDir, root: root}, maxBytes)
}

// includeRoot is the config root directory, held for the whole expansion
// with its resolved path (dir) pinned: every include is checked against dir
// and opened through root, so a root path, include path or parent directory
// replaced by a symlink leading outside after the checks cannot redirect a
// read.
type includeRoot struct {
	dir  string
	root *os.Root
	// given is the root path as the caller gave it, used only to report a
	// missing include outside the root as a traversal attempt.
	given string
	// owned reports a root opened here (closed by close); a root the caller
	// holds is left to it.
	owned bool
}

// pin opens the root directory, once, before the first include is checked,
// and pins its resolved path, refusing a directory replaced in between.
func (r *includeRoot) pin() error {
	if r.root != nil {
		return nil
	}
	root, err := files.OpenRootDir(r.dir)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(r.dir)
	if err == nil {
		err = sameDir(root, resolved)
	}
	if err != nil {
		_ = root.Close()
		return err
	}
	r.root, r.dir = root, resolved
	return nil
}

// sameDir reports an error unless path is the directory root holds.
func sameDir(root *os.Root, path string) error {
	held, err := root.Stat(".")
	if err != nil {
		return err
	}
	current, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(held, current) {
		return fmt.Errorf("security error: config root %s changed while it was opened", path)
	}
	return nil
}

// outsideDir reports whether path lies outside dir, lexically.
func outsideDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

// open opens rel, relative to the root directory, as a regular file.
func (r *includeRoot) open(rel string) (*os.File, error) {
	return files.OpenRegularIn(r.root, rel)
}

func (r *includeRoot) close() {
	if r.root != nil && r.owned {
		_ = r.root.Close()
	}
}

// processIncludes recursively processes !include directives in the given content.
// It handles indentation and prevents infinite loops with a depth limit and visited map.
// rootDir is used to prevent path traversal by ensuring all includes are within it.
// maxBytes, when positive, caps each include read and the expanded output.
func processIncludes(content string, currentDir string, depth int, visited map[string]bool, root *includeRoot, maxBytes int64) (string, error) {
	if depth >= maxIncludeDepth {
		return "", fmt.Errorf("max include depth reached (%d)", maxIncludeDepth)
	}

	var result strings.Builder
	result.Grow(len(content))
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := scanner.Text()
		matches := includeRegex.FindStringSubmatch(line)

		if len(matches) == includeRegexMatchGroups {
			included, err := expandInclude(matches[2], currentDir, depth, visited, root, maxBytes)
			if err != nil {
				return "", err
			}
			// Apply indentation to included content
			indentedContent := applyIndentation(included, matches[1])
			result.WriteString(indentedContent)
			if !strings.HasSuffix(indentedContent, "\n") {
				result.WriteString("\n")
			}
		} else {
			result.WriteString(line)
			result.WriteString("\n")
		}

		// Repeated includes of individually small files can expand the
		// document far beyond the size of any single file.
		if maxBytes > 0 && int64(result.Len()) > maxBytes {
			return "", coreerrs.Wrapf(coreio.ErrReadLimitExceeded, "expanded config exceeds %d bytes", maxBytes)
		}
	}

	if err := scanner.Err(); err != nil {
		return "", coreerrs.Wrap(err, "error scanning content")
	}

	return result.String(), nil
}

// expandInclude reads the file named by an !include directive's raw path
// argument, after the security checks of resolveInclude, and returns its
// content with its own includes expanded.
func expandInclude(rawPath, currentDir string, depth int, visited map[string]bool, root *includeRoot, maxBytes int64) (string, error) {
	includePath, realIncludePath, rel, err := resolveInclude(rawPath, currentDir, root)
	if err != nil {
		return "", err
	}

	// Check for circular includes using the resolved path so
	// two symlinks pointing to the same file are detected.
	if visited[realIncludePath] {
		return "", fmt.Errorf("circular include detected: %s", includePath)
	}

	// Create a new map for recursive calls to avoid side effects on the same level
	newVisited := make(map[string]bool, len(visited)+1)
	maps.Copy(newVisited, visited)
	newVisited[realIncludePath] = true

	// Read the RESOLVED file through the root: a symlink swapped
	// in between the checks above and the open cannot lead
	// outside the root (os.Root refuses the escape).
	includedContent, err := readInclude(root, rel, maxBytes)
	if err != nil {
		return "", coreerrs.WrapOperation(err, "read included file "+includePath)
	}

	// Recursively process includes in the included file
	return processIncludes(string(includedContent), filepath.Dir(realIncludePath), depth+1, newVisited, root, maxBytes)
}

// resolveInclude resolves an !include directive's raw path argument against
// currentDir and checks it: a YAML extension on both the written and the
// resolved path, and a resolved target inside the pinned root. It returns the
// cleaned written path (for messages), the resolved path and the resolved
// path relative to the root.
func resolveInclude(rawPath, currentDir string, root *includeRoot) (includePath, realIncludePath, rel string, err error) {
	includePath = strings.TrimSpace(rawPath)

	// Resolve relative path
	if !filepath.IsAbs(includePath) {
		includePath = filepath.Join(currentDir, includePath)
	}

	// Clean path to handle .. and .
	includePath = filepath.Clean(includePath)

	// Security check: validate extension on the symlink path.
	// A second extension check on the RESOLVED target runs below
	// — both are needed: the symlink-name check catches obvious
	// "!include foo.txt" mistakes at parse time, the resolved
	// check catches symlinks that lie about their target.
	ext := strings.ToLower(filepath.Ext(includePath))
	if ext != ".yaml" && ext != ".yml" {
		return includePath, "", "", fmt.Errorf("security error: included file %s has invalid extension (only .yaml, .yml allowed)", includePath)
	}

	// Resolve symlinks on the include path BEFORE the traversal
	// check. Without this, a symlink `legit.yaml -> /etc/shadow`
	// placed inside rootDir would pass both the extension check
	// (symlink name ends in .yaml) and the filepath.Rel-based
	// traversal check (the cleaned path resides inside
	// rootDir), and os.ReadFile would then follow the symlink
	// and inline the secret file. EvalSymlinks collapses every
	// link in the chain to the real target so security checks
	// operate on what will actually be read.
	//
	// When EvalSymlinks fails (missing file, broken symlink,
	// virtual path) we fall back to the cleaned includePath:
	// the downstream Rel-check + ReadFile will produce the same
	// errors they always did. This keeps the user-facing error
	// contract stable for the non-symlink cases — the symlink
	// attack itself REQUIRES the target to be readable, so a
	// failed EvalSymlinks could not have led to a successful
	// read either.
	//
	// Critical: resolve BOTH paths together or NEITHER. On
	// systems where /tmp is a symlink (e.g. /private/tmp on
	// macOS), mixing a resolved rootDir with an unresolved
	// includePath gives them different prefixes and the Rel
	// check rejects perfectly legal cases.
	//
	// The root is opened and its resolved path pinned before the
	// first include is checked, and that path is not resolved
	// again: an include resolving under a root path replaced
	// since fails the traversal check, and reads go through the
	// held root.
	if err = root.pin(); err != nil {
		return includePath, "", "", coreerrs.WrapOperation(err, "open config root for "+includePath)
	}
	realRootDir := root.dir
	realIncludePath, evalErr := filepath.EvalSymlinks(includePath)
	if evalErr != nil {
		// A missing include outside the root, as written, is
		// still reported as a traversal attempt.
		if outsideDir(root.dir, includePath) && outsideDir(root.given, includePath) {
			return includePath, "", "", fmt.Errorf("security error: attempt to include file outside of config root: %s", includePath)
		}
		return includePath, "", "", coreerrs.WrapOperation(evalErr, "read included file "+includePath)
	}

	// Security check: path traversal protection on RESOLVED paths.
	if outsideDir(realRootDir, realIncludePath) {
		return includePath, "", "", fmt.Errorf("security error: attempt to include file outside of config root: %s", includePath)
	}
	if rel, err = filepath.Rel(realRootDir, realIncludePath); err != nil {
		return includePath, "", "", fmt.Errorf("security error: attempt to include file outside of config root: %s", includePath)
	}

	// Security check: re-validate the extension on the
	// resolved target. The symlink name may end in .yaml while
	// the actual file does not — without this re-check, a
	// symlink inside rootDir pointing to (say) /etc/shadow
	// could still be inlined.
	realExt := strings.ToLower(filepath.Ext(realIncludePath))
	if realExt != ".yaml" && realExt != ".yml" {
		return includePath, "", "", fmt.Errorf("security error: included file %s resolves to non-YAML target %s", includePath, realIncludePath)
	}

	return includePath, realIncludePath, rel, nil
}

// readInclude reads the include target rel, relative to the root, bounded by
// maxBytes when it is positive. Only a regular file is read
// (files.OpenRegularIn): an include target that is a FIFO or a device would
// block or never end.
func readInclude(root *includeRoot, rel string, maxBytes int64) ([]byte, error) {
	f, err := root.open(rel)
	if err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		defer func() { _ = f.Close() }()
		return io.ReadAll(f)
	}
	limited := coreio.NewLimitedReadCloser(f, maxBytes)
	defer func() { _ = limited.Close() }()

	return io.ReadAll(limited)
}

// applyIndentation prepends the given indent to each line of the content,
// except for the first line and empty lines.
func applyIndentation(content, indent string) string {
	if indent == "" {
		return content
	}

	lines := strings.Split(content, "\n")
	result := make([]string, len(lines))

	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			result[i] = ""
		} else {
			result[i] = indent + line
		}
	}

	return strings.Join(result, "\n")
}
