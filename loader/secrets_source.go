// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/altessa-s/go-nomos/loader/backend"

	loadersecrets "github.com/altessa-s/go-nomos/loader/secrets"
)

var (
	// ErrSecretsUnsupported is returned when a configuration file holds a
	// $__secret{namespace:key} placeholder and its backend cannot resolve
	// placeholders at the source (it does not implement
	// backend.ScalarRewriter and backend.ScalarLister key nodes).
	ErrSecretsUnsupported = errors.New("backend does not support secret placeholders")

	// ErrSecretValueInvalid is returned when a value resolved from a secret
	// cannot be decoded or parsed into its field. The error names the
	// placeholders involved and where they are, never a secret's value: the
	// decoder's own error, which may quote it, is dropped.
	ErrSecretValueInvalid = errors.New("a value resolved from a secret does not decode")

	// ErrSecretInAnchor is returned when a secret placeholder lies inside an
	// anchored YAML node (scalar, mapping or sequence, at any depth) or in a
	// value merged with "<<": one source node would reach targets of
	// different types.
	ErrSecretInAnchor = errors.New("secret placeholder inside a shared node")

	// ErrSecretTagged is returned when a secret placeholder lies in a YAML
	// scalar with an explicit tag other than !!str (!!binary, !!int,
	// !!timestamp, a custom tag): the tag would decode the resolved value
	// differently from a plain string or number.
	ErrSecretTagged = errors.New("secret placeholder under an explicit tag")
)

// Secret placeholders are resolved at the source, when a value is taken from
// it and before it is parsed into a Go type, exactly once per source value:
//
//   - every placeholder of a file, in the parsed document, before the
//     document is decoded (expandFileSecrets), whether or not a later file or
//     the environment overrides the value: a missing secret in a file fails
//     the load;
//   - an environment value when it is assigned, for the spelling that won
//     (setEnvValue);
//   - a default tag when it is applied (applyTag).
//
// A resolved value is literal: parsing it (a collection, a number) never
// resolves anything again. A failure to decode or parse a value resolved from
// a secret is reported without the decoder's message, which may quote it.

// resolveSecrets resolves the placeholders of one source value.
func (cf *loadState) resolveSecrets(ctx context.Context, value string) (string, error) {
	if cf.options.secretResolver == nil || !loadersecrets.HasSecrets(value) {
		return value, nil
	}
	return loadersecrets.New(recordingResolver{inner: cf.options.secretResolver, cf: cf}).Expand(ctx, value)
}

// recordingResolver notes in cf.secretsResolved that a secret was served and
// bounds every lookup by the secrets timeout.
type recordingResolver struct {
	inner loadersecrets.Resolver
	cf    *loadState
}

// Resolve serves the secret reference, within its own lookup timeout, and
// notes that a secret was resolved.
func (r recordingResolver) Resolve(ctx context.Context, ref string) (string, error) {
	ctx, cancel := r.cf.options.getSecretsContext(ctx)
	defer cancel()
	v, err := r.inner.Resolve(ctx, ref)
	if err == nil {
		r.cf.secretsResolved = true
	}
	return v, err
}

// secretValueError reports that a value resolved from the placeholders refs,
// at where, does not decode. It carries no secret value.
func secretValueError(where string, refs []string) error {
	return fmt.Errorf("%w: %s (from %s)", ErrSecretValueInvalid, where, strings.Join(refs, ", "))
}

// sealParseError returns the parse error of a value resolved from the
// placeholders refs, at where: its message names the placeholders only (the
// parser's may quote the secret), and errors.Is still reaches
// ErrSecretValueInvalid and the parser's own errors (ErrBoolParseFailure,
// strconv.ErrSyntax, ErrMapItemInvalid ...), held privately and never
// unwrapped or formatted.
func sealParseError(err error, where string, refs []string) error {
	sanitized := secretValueError(where, refs)
	return &withheldError{msg: sanitized.Error(), original: errors.Join(sanitized, err)}
}

// expandFileSecrets resolves the secret placeholders of every file's scalars
// in its content. Whether a value holds a placeholder is decided on the
// parsed value, never on the raw text, where a quoted or escaped placeholder
// ("\x24__secret{...}") would not show: a supporting backend's document is
// always rewritten (RewriteScalars checks each parsed scalar); for another
// backend, a placeholder in any parsed string value fails the load.
func (cf *loadState) expandFileSecrets(ctx context.Context, files []*file) error {
	rootType := indirectType(cf.confType)
	for _, f := range files {
		// Hostile input (alias cycles, excessive aliasing) is rejected before
		// the keys are bound and walked: on the backend's own parse tree when
		// it can check one, otherwise by a generic decode, which a backend
		// without source rewriting also needs to find placeholders. A typed
		// decode would reject a placeholder for a number.
		var parsed any
		_, rewrites := f.decoder.(backend.ScalarRewriter)
		checker, checks := f.decoder.(backend.DocumentChecker)
		if checks && rewrites {
			if err := checker.CheckDocument(f.content); err != nil {
				return fmt.Errorf("%w: %s: %w", ErrDecode, f.name, err)
			}
		} else if err := f.decoder.Decode(strings.NewReader(f.content), &parsed); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrDecode, f.name, err)
		}
		if err := cf.bindKeys(f); err != nil {
			return err
		}
		// Without key nodes that list their scalars the backend cannot
		// rewrite at the source: the generic decode, skipped above, is what
		// finds placeholders, so a partial implementation fails closed.
		if _, lists := f.keys.(backend.ScalarLister); checks && rewrites && !lists {
			if err := f.decoder.Decode(strings.NewReader(f.content), &parsed); err != nil {
				return fmt.Errorf("%w: %s: %w", ErrDecode, f.name, err)
			}
		}
		if err := cf.expandFileContent(ctx, f, rootType, parsed); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrDecode, f.name, err)
		}
	}
	return nil
}

// holdsPlaceholder reports whether a parsed document value holds a string
// with a secret placeholder (map keys are not expanded, so not counted).
func holdsPlaceholder(v any) bool {
	switch v := v.(type) {
	case string:
		return loadersecrets.HasSecrets(v)
	case []byte:
		return loadersecrets.HasSecrets(string(v))
	case map[string]any:
		for _, e := range v {
			if holdsPlaceholder(e) {
				return true
			}
		}
	case map[any]any:
		for _, e := range v {
			if holdsPlaceholder(e) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(v, holdsPlaceholder)
	default:
		return holdsPlaceholderReflect(reflect.ValueOf(v))
	}
	return false
}

// holdsPlaceholderReflect is holdsPlaceholder for the values of a typed map
// and the elements of a typed slice or array.
func holdsPlaceholderReflect(rv reflect.Value) bool {
	switch rv.Kind() {
	case reflect.Map:
		for it := rv.MapRange(); it.Next(); {
			if holdsPlaceholder(it.Value().Interface()) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range rv.Len() {
			if holdsPlaceholder(rv.Index(i).Interface()) {
				return true
			}
		}
	default:
	}
	return false
}

// expandFileContent resolves the placeholders of f's scalars in f.content
// and records them in f.secretRefs. A literal target (a string, an
// interface) keeps the resolved value a string; a number or a bool parses
// it. A placeholder inside an anchored node or a "<<" merge is rejected: the
// node may reach targets of different types sharing one resolution.
func (cf *loadState) expandFileContent(ctx context.Context, f *file, rootType reflect.Type, parsed any) error {
	rewriter, ok := f.decoder.(backend.ScalarRewriter)
	if _, lists := f.keys.(backend.ScalarLister); !ok || !lists {
		if holdsPlaceholder(parsed) {
			return ErrSecretsUnsupported
		}
		return nil
	}
	literal := make(map[backend.Position]bool)
	collectTargets(f.keys, rootType, literal)

	content, err := rewriter.RewriteScalars(f.content, func(s backend.Scalar) (string, bool, bool, error) {
		if !loadersecrets.HasSecrets(s.Value) {
			return "", false, false, nil
		}
		refs := loadersecrets.References(s.Value)
		if s.Tag != "" && s.Tag != "!!str" {
			return "", false, false, fmt.Errorf("%w: line %d: secret placeholder (%s) under the tag %s",
				ErrSecretTagged, s.Pos.Line, strings.Join(refs, ", "), s.Tag)
		}
		if s.Shared {
			return "", false, false, fmt.Errorf("%w: line %d: secret placeholder (%s) inside an anchored node",
				ErrSecretInAnchor, s.Pos.Line, strings.Join(refs, ", "))
		}
		resolved, err := cf.resolveSecrets(ctx, s.Value)
		if err != nil {
			return "", false, false, fmt.Errorf("line %d: %w", s.Pos.Line, err)
		}
		f.secretRefs = append(f.secretRefs, fmt.Sprintf("line %d: %s", s.Pos.Line, strings.Join(refs, ", ")))
		isLiteral, known := literal[s.Pos]
		keepString := isLiteral || !known || s.Tag == "!!str"
		if !keepString && !utf8.ValidString(resolved) {
			// A number or a bool cannot be invalid UTF-8; the document
			// could not even carry it as plain text.
			return "", false, false, fmt.Errorf("line %d: %w", s.Pos.Line, secretValueError("the value", refs))
		}
		return resolved, keepString, true, nil
	})
	if err != nil {
		return err
	}
	f.unresolved, f.content = f.content, content
	return nil
}

// collectTargets records in literal the scalars of node, bound to type t,
// with whether their target keeps a value a string (true) or parses it (a
// number or a bool).
func collectTargets(node backend.KeyNode, t reflect.Type, literal map[backend.Position]bool) {
	if node == nil {
		return
	}
	t = derefType(t)
	switch t.Kind() {
	case reflect.Struct:
		if fields, ok := node.Fields(t); ok {
			for i, child := range fields {
				collectTargets(child, t.Field(i).Type, literal)
			}
			return
		}
	case reflect.Map:
		if entries, ok := node.Entries(t); ok {
			for _, child := range entries {
				collectTargets(child, t.Elem(), literal)
			}
			return
		}
	case reflect.Slice, reflect.Array:
		if elems, ok := node.Elems(t); ok {
			for _, child := range elems {
				collectTargets(child, t.Elem(), literal)
			}
			return
		}
	default:
	}
	lister, ok := node.(backend.ScalarLister)
	if !ok {
		return
	}
	for _, pos := range lister.Scalars() {
		literal[pos] = keepsString(t)
	}
}

// keepsString reports whether a value of kind t keeps a resolved scalar as a
// string rather than parsing it: anything but a number or a bool.
func keepsString(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return false
	default:
		return true
	}
}
