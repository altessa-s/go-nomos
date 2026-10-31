// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/altessa-s/go-nomos/loader/backend"
)

// Backend is a YAML file backend for the configuration parser.
// It uses gopkg.in/yaml.v3 for parsing.
//
// Example:
//
//	p := parser.New(&yaml3.Backend{}, parser.WithPath("config.yaml"))
type Backend struct{}

// errEncode reports a value Encode cannot serialize.
var errEncode = errors.New("yaml: cannot encode value")

// StructTagName returns the struct tag name for YAML.
func (b *Backend) StructTagName() string {
	return "yaml"
}

// FileExtensions returns the YAML file extensions.
func (b *Backend) FileExtensions() []string {
	return []string{"yaml", "yml"}
}

// Decode decodes the YAML data from the reader.
func (b *Backend) Decode(reader io.Reader, in any) error {
	reader, err := readChecked(reader)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(reader)
	return decoder.Decode(in)
}

// Ensure Backend implements backend.Backend.
var _ backend.Backend = (*Backend)(nil)

// PrepareDocument normalizes empty or comments-only YAML into an empty mapping.
func (b *Backend) PrepareDocument(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return content
		}
	}
	return "{}\n"
}

// Encode serializes a configuration value to a writer. A value yaml.v3
// cannot marshal, such as a channel, is an error, not a panic.
func (b *Backend) Encode(w io.Writer, value any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", errEncode, r)
		}
	}()
	e := yaml.NewEncoder(w)
	const indent = 4
	e.SetIndent(indent)
	if err := e.Encode(value); err != nil {
		return err
	}
	return e.Close()
}
