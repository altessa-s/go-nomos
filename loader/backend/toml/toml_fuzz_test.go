// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package toml_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	toml "github.com/altessa-s/go-nomos/loader/backend/toml"
)

// config is the destination a loader decodes into.
type config struct {
	Key   string `toml:"key"`
	Num   int    `toml:"num"`
	Value any    `toml:"value"`
}

// FuzzDecodeIsDeterministic pins what the Decode contract guarantees: it never
// panics, and the same document decoded from the same starting state always
// gives the same outcome — refused both times, or accepted both times with
// equal values.
//
// Decode writes straight into the destination, so a refused document may leave
// it partially populated (for example `key = "set"` followed by a type mismatch
// sets Key before failing). Rolling back is the loader's job, not the backend's,
// so this target does not assert atomicity. There is deliberately no round-trip
// target here: Decode delegates to the TOML library, so a value that does not
// survive re-encoding says something about that library's type mapping rather
// than about this package.
func FuzzDecodeIsDeterministic(f *testing.F) {
	f.Add([]byte(`key = "value"`))
	f.Add([]byte(""))
	f.Add([]byte("[[array]]\nid = 1"))
	f.Add([]byte(`nested.key = "value"`))
	f.Add([]byte("num = notanumber"))
	f.Add([]byte("key = "))
	f.Add([]byte(strings.Repeat("[a]\n", 64)))
	f.Add([]byte("\x00"))
	f.Add([]byte("key = \"set\"\nnum = \"text\""))

	backend := &toml.Backend{}

	f.Fuzz(func(t *testing.T, data []byte) {
		sentinel := config{Key: "untouched", Num: -1}

		// The loader reads the same file again on a hot reload, and a document
		// accepted once and refused the next time swaps the running
		// configuration for the previous one. Both decodes start from the same
		// state: TOML leaves fields the document does not mention untouched, so
		// comparing against a zero value would measure the fixture rather than
		// the backend.
		target, again := sentinel, sentinel
		targetErr := backend.Decode(bytes.NewReader(data), &target)
		againErr := backend.Decode(bytes.NewReader(data), &again)
		require.Equal(t, targetErr == nil, againErr == nil,
			"decoding the same document twice gave different outcomes (%v, %v): %q", targetErr, againErr, data)
		if targetErr != nil {
			return
		}
		// %#v keeps NaN comparable and prints map keys in sorted order.
		require.Equal(t, fmt.Sprintf("%#v", target), fmt.Sprintf("%#v", again),
			"decoding the same document twice gave two values: %q", data)
	})
}
