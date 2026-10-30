// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3_test

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	yaml3 "github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

// literalMarker is a regression seed: a document that merely spells out a
// marker string must pass, because the oracle tracks provenance rather than text.
const literalMarker = "THIS-IS-OUTSIDE-THE-ROOT"

// FuzzPreprocessNeverReadsOutsideTheRoot is the include-traversal oracle.
//
// `!include` inlines a file into the configuration, and the root directory is
// the only thing stopping a directive from naming one that was never meant to
// be configuration. The guard is genuinely intricate — an extension check, a
// symlink resolution, and a filepath.Rel containment test that must agree about
// which path they are talking about — and a config file is not always fully
// trusted: it is templated, assembled by a deploy tool, or edited by someone
// with narrower access than the process has.
//
// The check is not on the returned error but on the returned text: whatever
// happens, the bytes of a file outside the root must never appear in it. The
// outside file carries a fresh random token per run, so the input cannot spell
// it out and an appearance in the output can only come from reading the file.
func FuzzPreprocessNeverReadsOutsideTheRoot(f *testing.F) {
	f.Add("!include ../outside.yaml")
	f.Add("!include ./inside.yaml")
	f.Add("!include /etc/passwd")
	f.Add("!include ../../outside.yaml")
	f.Add("  !include ../outside.yaml")
	f.Add("!include outside.yaml/../../outside.yaml")
	f.Add("key: value")
	f.Add("")
	f.Add("!include link.yaml")
	f.Add(literalMarker)

	f.Fuzz(func(t *testing.T, content string) {
		secretContent := "OUTSIDE-" + rand.Text()
		base := t.TempDir()
		root := filepath.Join(base, "root")
		require.NoError(t, os.MkdirAll(root, 0o750))

		// A readable YAML file the directive is allowed to reach.
		require.NoError(t, os.WriteFile(filepath.Join(root, "inside.yaml"),
			[]byte("inside: ok\n"), 0o600))

		// The same, one level up — outside the root, and therefore off limits.
		outside := filepath.Join(base, "outside.yaml")
		require.NoError(t, os.WriteFile(outside, []byte("secret: "+secretContent+"\n"), 0o600))

		// A symlink inside the root pointing at it, which is the case the
		// resolution step exists for.
		_ = os.Symlink(outside, filepath.Join(root, "link.yaml"))

		backend := &yaml3.Backend{}
		out, err := backend.Preprocess(content, root, root, 0)
		if err != nil {
			require.Empty(t, out, "a rejected document must not also produce output")
			return
		}

		require.NotContains(t, out, secretContent,
			"an include reached outside the root:\n%s", out)
	})
}

// FuzzDecodeIsDeterministic pins what the Decode contract guarantees: it never
// panics, and the same document always gives the same outcome — refused both
// times, or accepted both times with equal values.
//
// Decode writes straight into the destination, so a refused document may leave
// it partially populated (for example "a: 1\nb: !!int nope\n" sets a before
// failing). Rolling back is the loader's job, not the backend's, so this target
// does not assert atomicity. Note the deliberate absence of a round-trip target
// here: Decode delegates straight to yaml.v3, so a value that does not survive
// re-encoding says something about the library's number typing rather than
// about this package.
func FuzzDecodeIsDeterministic(f *testing.F) {
	f.Add([]byte("key: value"))
	f.Add([]byte(""))
	f.Add([]byte("a: [1, 2, 3]"))
	f.Add([]byte("a: &x 1\nb: *x"))
	f.Add([]byte("0: [08]"))
	f.Add([]byte("!"))
	f.Add([]byte("\x00"))
	f.Add([]byte(strings.Repeat("a: &a [*a]\n", 4)))
	f.Add([]byte("a: 1\nb: !!int nope\n"))

	backend := &yaml3.Backend{}

	f.Fuzz(func(t *testing.T, data []byte) {
		var first, again map[string]any
		firstErr := backend.Decode(bytes.NewReader(data), &first)
		againErr := backend.Decode(bytes.NewReader(data), &again)

		// The loader reads the file more than once on a hot reload, so the
		// outcome must not depend on which read it is.
		require.Equal(t, firstErr == nil, againErr == nil,
			"decoding the same document twice gave different outcomes (%v, %v): %q", firstErr, againErr, data)
		if firstErr != nil {
			return
		}
		// %#v keeps NaN comparable and prints map keys in sorted order.
		require.Equal(t, fmt.Sprintf("%#v", first), fmt.Sprintf("%#v", again),
			"decoding the same document twice gave two values: %q", data)
	})
}
