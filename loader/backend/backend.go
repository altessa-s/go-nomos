// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package backend

import (
	"errors"
	"io"
	"os"
	"reflect"
)

// ErrUnknownField is wrapped by a [StrictDecoder] error when the document sets
// keys that bind to no field of the destination struct, typically a misspelled
// key such as "enable" for "enabled".
var ErrUnknownField = errors.New("unknown field")

// Decoder is the interface that should be implemented by a backend to decode the data from the reader.
// Implementations should parse the configuration format into the provided struct.
type Decoder interface {
	Decode(reader io.Reader, in any) error
}

// Backend is the interface that should be implemented by a configuration backend.
// It provides format-specific decoding and file extension information.
type Backend interface {
	Decoder

	// FileExtensions should return the file extensions that the backend supports.
	FileExtensions() []string

	// StructTagName should return the struct tag name for the backend.
	// This is used to decode the struct tags.
	StructTagName() string
}

// Preprocessor is the interface that should be implemented by a backend that supports
// pre-processing of the configuration content before decoding.
type Preprocessor interface {
	// Preprocess performs pre-processing on the provided content.
	// currentDir is the directory of the file being processed.
	// rootDir is the root directory for security checks.
	// maxBytes, when positive, bounds every file the preprocessor reads and
	// the size of the returned content.
	Preprocess(content, currentDir, rootDir string, maxBytes int64) (string, error)
}

// RootPreprocessor is a Preprocessor that reads the files it includes through
// a root directory the caller holds open: replacing the root's path after the
// caller opened it cannot redirect the reads. rootDir is the root's path as
// resolved when it was opened, used for the include path checks.
type RootPreprocessor interface {
	PreprocessIn(content, currentDir string, root *os.Root, rootDir string, maxBytes int64) (string, error)
}

// StrictDecoder is implemented by a backend that can reject document keys
// that bind to no field of a destination struct. DecodeStrict decodes like
// Decode but fails with an error wrapping [ErrUnknownField] that names every
// such key. Map entry keys are never unknown, though struct values in a map
// are checked; content under interfaces and types with their own unmarshaler
// is free-form.
type StrictDecoder interface {
	DecodeStrict(reader io.Reader, in any) error
}

// KeyDecoder is implemented by a backend that can report which values a
// document sets. The loader uses it to tell an explicit false, 0 or "" from an
// omitted key, so default tags apply only to the latter.
type KeyDecoder interface {
	// DecodeKeys parses the document and returns its root value; an empty
	// document is a null value.
	DecodeKeys(reader io.Reader) (KeyNode, error)
}

// KeyNode is a value of a parsed document. Its children are bound to
// destination types on demand, exactly the way the backend's Decode binds them:
// struct fields by the backend's key, embedding and case rules; map keys
// decoded into the map's key type; sequence elements by the position they
// take in the decoded slice.
type KeyNode interface {
	// IsNull reports whether the value is an explicit null.
	IsNull() bool

	// Fields binds the value to struct type t and returns the values of the
	// fields it sets, keyed by field index in t. An anonymous or inline field
	// whose fields the backend flattens into t maps to a node whose Fields
	// returns them. It returns false when the value does not bind to t
	// field by field, e.g. when t decodes itself through an unmarshaler.
	Fields(t reflect.Type) (map[int]KeyNode, bool)

	// Entries binds the value to map type t and returns its entries keyed by
	// the decoded key (a value of t's key type), or false when it does not
	// bind to t.
	Entries(t reflect.Type) (map[any]KeyNode, bool)

	// Elems binds the value to slice or array type t and returns its elements
	// in decoded order, or false when it does not bind to t.
	Elems(t reflect.Type) ([]KeyNode, bool)
}

// Position locates a scalar value in a document: its line and column, both
// starting at 1. Two scalars of one document never share a position.
type Position struct {
	Line   int
	Column int
}

// Scalar is one scalar value of a document offered to a ScalarRewrite.
type Scalar struct {
	// Pos locates the scalar.
	Pos Position
	// Value is its decoded text: the text the backend decodes into a string
	// (for a YAML !!binary scalar, the decoded bytes).
	Value string
	// Tag is the scalar's explicit tag in short form ("!!binary", "!foo"),
	// or "" when the tag is resolved implicitly.
	Tag string
	// Shared reports a scalar that may reach several destinations: it lies
	// inside an anchored node (scalar, mapping or sequence, at any depth)
	// or in a value merged into a mapping (YAML "<<").
	Shared bool
}

// ScalarRewrite decides the new value of one scalar of a document. ok false
// leaves the scalar as it is. literal true keeps the new value a string;
// false lets the backend resolve its type from the text, as for a plain
// scalar (a number, a bool).
type ScalarRewrite func(s Scalar) (rewritten string, literal, ok bool, err error)

// ScalarRewriter is implemented by a backend that can rewrite the scalar values
// of a document before it is decoded; the loader resolves secret placeholders
// with it, at the source. Every scalar node is offered once, however many
// aliases refer to it; mapping keys are not offered.
type ScalarRewriter interface {
	// RewriteScalars parses content, applies rewrite to its scalars and
	// returns the re-encoded document, or content itself when no scalar
	// changed.
	RewriteScalars(content string, rewrite ScalarRewrite) (string, error)
}

// ScalarLister is implemented by a KeyNode that can list the positions of the
// scalar values at or below it (mapping keys excluded), following aliases.
type ScalarLister interface {
	Scalars() []Position
}

// DocumentChecker is implemented by a backend that can check a document for
// structural hazards (alias cycles, excessive aliasing) on its own parse
// tree, without decoding it into Go values. The loader runs it before
// walking a document to resolve secret placeholders; a backend without it is
// checked with a generic Decode.
type DocumentChecker interface {
	CheckDocument(content string) error
}

// SliceReuseDecoder describes decoders that reuse existing slice elements while
// decoding a later document. Decoders without it replace slice elements.
// The loader uses this policy to merge explicit-value presence consistently.
type SliceReuseDecoder interface {
	ReusesSliceElements() bool
}

// DocumentPreparer normalizes format-specific empty documents before decoding.
type DocumentPreparer interface{ PrepareDocument(content string) string }
