// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/altessa-s/go-nomos/loader/backend"
)

const (
	// yamlIndent is the indentation of a re-encoded document.
	yamlIndent = 2
	// mergeKey is the YAML merge key and mergeTag its tag.
	mergeKey = "<<"
	mergeTag = "!!merge"
)

// RewriteScalars parses the first YAML document of content and offers each
// of its scalar nodes, mapping keys excluded, to rewrite once: an anchored
// node once however many aliases refer to it, reported Shared when it lies in
// an anchored node or in a value merged with "<<". A rewritten literal value is
// stored as a double-quoted string, any other as a plain scalar whose type
// yaml.v3 resolves (8080 is an int, true a bool). The document is re-encoded
// only when a scalar changed.
func (b *Backend) RewriteScalars(content string, rewrite backend.ScalarRewrite) (string, error) {
	var doc yaml.Node
	if err := yaml.NewDecoder(strings.NewReader(content)).Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return content, nil
		}
		return "", err
	}

	w := &scalarWalker{rewrite: rewrite, visited: make(map[*yaml.Node]bool)}
	if err := w.walk(&doc, false); err != nil {
		return "", err
	}
	if !w.changed {
		return content, nil
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(&doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// scalarWalker offers the scalar nodes of a document to rewrite, each once.
type scalarWalker struct {
	rewrite backend.ScalarRewrite
	visited map[*yaml.Node]bool
	changed bool
}

// walk visits n and its descendants; shared reports a node inside an
// anchored node or merged with "<<": one source node reaching several
// targets.
func (w *scalarWalker) walk(n *yaml.Node, shared bool) error {
	if n == nil || w.visited[n] {
		return nil
	}
	w.visited[n] = true
	shared = shared || n.Anchor != ""
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			if err := w.walk(c, shared); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 1; i < len(n.Content); i += 2 {
			merged := isMerge(n.Content[i-1])
			if err := w.walk(n.Content[i], shared || merged); err != nil {
				return err
			}
		}
	case yaml.AliasNode:
		return w.walk(n.Alias, true)
	case yaml.ScalarNode:
		return w.rewriteScalar(n, shared)
	default:
	}
	return nil
}

// rewriteScalar offers scalar n to rewrite and stores a rewritten value.
func (w *scalarWalker) rewriteScalar(n *yaml.Node, shared bool) error {
	out, literal, ok, err := w.rewrite(backend.Scalar{
		Pos:    backend.Position{Line: n.Line, Column: n.Column},
		Value:  decodedText(n),
		Tag:    explicitTag(n),
		Shared: shared,
	})
	if err != nil || !ok {
		return err
	}
	switch {
	case literal && !utf8.ValidString(out):
		// yaml.v3 encodes no invalid UTF-8 as text: carry the
		// bytes as !!binary, which it decodes into strings,
		// interfaces, byte slices and text unmarshalers.
		n.Value, n.Tag, n.Style = base64.StdEncoding.EncodeToString([]byte(out)), binaryTag, 0
	case literal:
		n.Value, n.Tag, n.Style = out, "!!str", yaml.DoubleQuotedStyle
	default:
		n.Value, n.Tag, n.Style = out, "", 0
	}
	w.changed = true
	return nil
}

// isMerge reports whether a mapping key is the merge key, by yaml.v3's own
// rule: the scalar "<<" with an empty, non-specific or !!merge tag. A quoted
// or !!str "<<" is an ordinary key.
func isMerge(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Value == mergeKey && (n.Tag == "" || n.Tag == "!" || n.ShortTag() == mergeTag)
}

// binaryTag is the YAML tag of base64-encoded binary data.
const binaryTag = "!!binary"

// explicitTag returns the tag written on the scalar in short form, or "" when
// yaml.v3 resolved it implicitly.
func explicitTag(n *yaml.Node) string {
	if n.Style&yaml.TaggedStyle == 0 {
		return ""
	}
	return n.ShortTag()
}

// decodedText returns the text a scalar decodes to as a string: its value,
// except for !!binary, whose base64 is decoded (an invalid encoding is
// returned as it is; decoding the document rejects it).
func decodedText(n *yaml.Node) string {
	if n.ShortTag() != binaryTag {
		return n.Value
	}
	b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(n.Value), ""))
	if err != nil {
		return n.Value
	}
	return string(b)
}

// Scalars lists the positions of the scalar values at or below the node,
// mapping keys excluded, following aliases (each anchored node once).
func (k *keyNode) Scalars() []backend.Position {
	n, _, ok := k.target()
	if !ok || n == nil {
		return nil
	}
	var out []backend.Position
	visited := make(map[*yaml.Node]bool)
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil || visited[n] {
			return
		}
		visited[n] = true
		switch n.Kind {
		case yaml.DocumentNode, yaml.SequenceNode:
			for _, c := range n.Content {
				walk(c)
			}
		case yaml.MappingNode:
			for i := 1; i < len(n.Content); i += 2 {
				walk(n.Content[i])
			}
		case yaml.AliasNode:
			walk(n.Alias)
		case yaml.ScalarNode:
			out = append(out, backend.Position{Line: n.Line, Column: n.Column})
		default:
		}
	}
	walk(n)
	return out
}

// Scalars lists the scalar positions of an inline struct's fields.
func (n *fieldsNode) Scalars() []backend.Position {
	var out []backend.Position
	for _, f := range n.fields {
		if l, ok := f.(backend.ScalarLister); ok {
			out = append(out, l.Scalars()...)
		}
	}
	return out
}

// Scalars lists the scalar positions of an inline map's entries.
func (n *entriesNode) Scalars() []backend.Position {
	var out []backend.Position
	for _, e := range n.entries {
		if l, ok := e.(backend.ScalarLister); ok {
			out = append(out, l.Scalars()...)
		}
	}
	return out
}

// Ensure Backend implements backend.ScalarRewriter and its nodes
// backend.ScalarLister.
var (
	_ backend.ScalarRewriter = (*Backend)(nil)
	_ backend.ScalarLister   = (*keyNode)(nil)
)
