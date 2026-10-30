// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package toml

import (
	"io"

	"github.com/BurntSushi/toml"

	"github.com/altessa-s/go-nomos/loader/backend"
)

// Backend is a TOML file backend for the configuration parser.
// It uses github.com/BurntSushi/toml for parsing.
//
// Example:
//
//	p := parser.New(&toml.Backend{}, parser.WithPath("config.toml"))
type Backend struct{}

// StructTagName returns the struct tag name for TOML.
func (b *Backend) StructTagName() string {
	return "toml"
}

// FileExtensions returns the TOML file extensions.
func (b *Backend) FileExtensions() []string {
	return []string{"toml", "tml"}
}

// Decode decodes the TOML data from the reader.
func (b *Backend) Decode(reader io.Reader, in any) error {
	_, err := toml.NewDecoder(reader).Decode(in)
	return err
}

// Ensure Backend implements backend.Backend.
var _ backend.Backend = (*Backend)(nil)

// ReusesSliceElements reports TOML's reuse of existing slice elements.
func (b *Backend) ReusesSliceElements() bool { return true }

var _ backend.SliceReuseDecoder = (*Backend)(nil)

// Encode serializes a configuration value to a writer.
func (b *Backend) Encode(w io.Writer, value any) error {
	return toml.NewEncoder(w).Encode(value)
}
