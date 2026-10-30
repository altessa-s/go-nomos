// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/altessa-s/go-nomos/loader/backend"

	coreio "github.com/altessa-s/go-atlas-core/io"
	corefiles "github.com/altessa-s/go-atlas-core/io/files"
	loadersecrets "github.com/altessa-s/go-nomos/loader/secrets"
)

// files manages a collection of configuration files with thread-safe access.
type files struct {
	files map[string]*file
	mx    sync.RWMutex
}

// file represents a single configuration file with its metadata and decoder.
type file struct {
	decoder backend.Decoder
	name    string
	path    string
	// rel, when set, is the file's resolved path relative to the configured
	// directory (the loader's root): the file is opened through the root, so
	// a path replaced by a symlink leading outside after discovery cannot
	// escape the directory. A single configured file has none.
	rel string
	// keys is the root value of the last loaded content, from which the
	// loader learns the values the file wrote explicitly.
	keys backend.KeyNode
	// content is the file's text after preprocessing and $VAR substitution
	// and, with a secrets manager, after its secret placeholders were
	// resolved (see expandFileSecrets).
	content string
	// secretRefs lists, by line, the placeholders resolved in content; a
	// decode error of such content is reported without the decoder's
	// message, which may quote a secret.
	secretRefs []string
	// unresolved is content before its placeholders were resolved, used to
	// tell whether a decode error concerns a secret value.
	unresolved string
}

// readContent reads the file into f.content: it opens the file, then
// preprocesses and substitutes its content.
func (cf *loadState) readContent(f *file) (err error) {
	var osFile *os.File

	// Only a regular file is read: opening a FIFO without a writer blocks
	// forever, and a device may never end. OpenRegular opens without
	// blocking and checks the opened descriptor.
	if f.rel != "" {
		osFile, err = corefiles.OpenRegularIn(cf.root, f.rel)
	} else {
		osFile, err = corefiles.OpenRegular(f.path)
	}
	if err != nil {
		err = fmt.Errorf("%w: %s: %w", ErrDecode, f.name, err)
		return
	}

	defer func() {
		_ = osFile.Close()
	}()

	// Read file content for environment variable substitution. The read
	// is bounded by maxConfigBytes (default DefaultMaxConfigBytes, 16
	// MiB): without a cap, a symlink to /dev/zero or a multi-GB tmpfs
	// file would OOM the loader before the YAML parser ever rejected
	// the input. Operators with genuinely huge configs can raise the
	// cap via WithMaxConfigBytes.
	maxBytes := cf.options.maxConfigBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxConfigBytes
	}
	limited := coreio.NewLimitedReadCloser(osFile, maxBytes)
	content, err := io.ReadAll(limited)
	if err != nil && err != io.EOF {
		err = fmt.Errorf("%w: %s: %w", ErrDecode, f.name, err)
		return
	}
	// Clear error if it was just EOF (empty file is OK)
	if err == io.EOF {
		err = nil
	}

	// Perform pre-processing if supported by the backend
	processedContent := string(content)
	if preprocessor, ok := f.decoder.(backend.Preprocessor); ok {
		// Includes are confined to the config root held since discovery,
		// read through it when the backend can.
		if rp, ok := preprocessor.(backend.RootPreprocessor); ok {
			processedContent, err = rp.PreprocessIn(string(content), filepath.Dir(f.path), cf.root, cf.rootDir, maxBytes)
		} else {
			processedContent, err = preprocessor.Preprocess(string(content), filepath.Dir(f.path), cf.rootDir, maxBytes)
		}
		if err != nil {
			err = fmt.Errorf("%w: %s: %w", ErrDecode, f.name, err)
			return
		}
	}

	// Perform environment variable substitution on raw text
	var substitutedContent string
	if cf.options.strict {
		substitutedContent, err = substituteEnvVariablesStrict(processedContent)
		if err != nil {
			err = fmt.Errorf("%w: %s: %w", ErrDecode, f.name, err)
			return
		}
	} else {
		substitutedContent = substituteEnvVariables(processedContent)
	}

	if p, ok := f.decoder.(backend.DocumentPreparer); ok {
		substitutedContent = p.PrepareDocument(substitutedContent)
	}
	f.content = substitutedContent
	return nil
}

// decodeContent decodes f.content into out and records which keys it set in
// f.keys.
func (cf *loadState) decodeContent(f *file, out any) (err error) {
	substitutedContent := f.content

	// Decode the same content generically to record which keys were set.
	// Create a reader from the substituted content
	substitutedReader := strings.NewReader(substitutedContent)

	if sd, ok := f.decoder.(backend.StrictDecoder); ok && !cf.options.allowUnknownFields {
		err = sd.DecodeStrict(substitutedReader, out)
	} else {
		err = f.decoder.Decode(substitutedReader, out)
	}
	if err != nil {
		if len(f.secretRefs) > 0 {
			return cf.sealDecodeError(f, out, err)
		}
		err = fmt.Errorf("%w: %s: %w", ErrDecode, f.name, err)
		return
	}

	// Record which keys the file set. This runs only after the typed decode
	// accepted the document, so the decoder's own limits (alias cycles,
	// excessive aliasing) have already rejected hostile input.
	return cf.bindKeys(f)
}

// sealDecodeError returns the decode error of a file whose content holds
// resolved secrets: the decoder's message may quote one, so it is withheld,
// while errors.Is still reaches ErrDecode and the decoder's own errors
// (ErrUnknownField, an unmarshaler's sentinel). ErrSecretValueInvalid is
// added only when the failure concerns a secret value: the document decodes
// once its secret-derived values are left out (null).
func (cf *loadState) sealDecodeError(f *file, out any, err error) error {
	original := fmt.Errorf("%w: %s: %w", ErrDecode, f.name, err)
	msg := fmt.Sprintf("%s: %s: decoding failed (secrets were resolved; details withheld)", ErrDecode, f.name)
	if cf.decodesWithoutSecrets(f, reflect.TypeOf(out)) {
		original = fmt.Errorf("%w; %w", original, secretValueError("a value", f.secretRefs))
		msg = fmt.Sprintf("%s: %s: %s", ErrDecode, f.name, secretValueError("a value", f.secretRefs))
	}
	return &withheldError{msg: msg, original: original}
}

// decodesWithoutSecrets reports whether f's unresolved content decodes into
// a fresh value of type t once every scalar holding a placeholder is null.
func (cf *loadState) decodesWithoutSecrets(f *file, t reflect.Type) bool {
	rewriter, ok := f.decoder.(backend.ScalarRewriter)
	if !ok || t == nil {
		return false
	}
	content, err := rewriter.RewriteScalars(f.unresolved, func(s backend.Scalar) (string, bool, bool, error) {
		return "", false, loadersecrets.HasSecrets(s.Value), nil
	})
	if err != nil {
		return false
	}
	scratch := reflect.New(indirectType(t)).Interface()
	reader := strings.NewReader(content)
	if sd, ok := f.decoder.(backend.StrictDecoder); ok && !cf.options.allowUnknownFields {
		return sd.DecodeStrict(reader, scratch) == nil
	}
	return f.decoder.Decode(reader, scratch) == nil
}

// bindKeys records in f.keys the values f.content sets.
func (cf *loadState) bindKeys(f *file) (err error) {
	if kd, ok := f.decoder.(backend.KeyDecoder); ok {
		f.keys, err = kd.DecodeKeys(strings.NewReader(f.content))
	} else {
		var generic map[string]any
		err = f.decoder.Decode(strings.NewReader(f.content), &generic)
		f.keys = genericNode{value: generic, tag: cf.backend.StructTagName()}
	}
	if err != nil {
		err = fmt.Errorf("%w: %s: %w", ErrDecode, f.name, err)
	}
	return err
}

// newFiles creates a new files instance with an initialized map.
func newFiles() *files {
	return &files{
		files: make(map[string]*file),
	}
}

// add adds a new file to the internal map if it doesn't already exist.
// This method is thread-safe.
func (fs *files) add(f *file) {
	fs.mx.Lock()
	defer fs.mx.Unlock()

	if _, ok := fs.files[f.name]; !ok {
		fs.files[f.name] = f
	}
}

// All returns an iterator for the files collection.
// It ensures thread-safe access during iteration by holding a read lock.
func (fs *files) All() iter.Seq[*file] {
	return func(yield func(*file) bool) {
		fs.mx.RLock()
		ordered := make([]*file, 0, len(fs.files))
		for _, f := range fs.files {
			ordered = append(ordered, f)
		}
		fs.mx.RUnlock()

		slices.SortFunc(ordered, func(a, b *file) int {
			if a.name != b.name {
				if a.name < b.name {
					return -1
				}
				return 1
			}
			if a.path < b.path {
				return -1
			}
			if a.path > b.path {
				return 1
			}
			return 0
		})

		for _, f := range ordered {
			if !yield(f) {
				return
			}
		}
	}
}
