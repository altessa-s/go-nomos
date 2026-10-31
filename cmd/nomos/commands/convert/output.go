// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/toml"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
)

type codec interface {
	backend.Decoder
	Encode(io.Writer, any) error
}

func formatCodec(format string) (codec, error) {
	switch format {
	case formatYAML:
		return &yaml3.Backend{}, nil
	case formatTOML:
		return &toml.Backend{}, nil
	}
	return nil, fmt.Errorf("unsupported format: %s", format)
}
func decodeConfig(format string, data []byte, out any) error {
	c, err := formatCodec(format)
	if err != nil {
		return err
	}
	if p, ok := c.(backend.DocumentPreparer); ok {
		data = []byte(p.PrepareDocument(string(data)))
	}
	return c.Decode(bytes.NewReader(data), out)
}
func marshalConfig(format string, data any) ([]byte, error) {
	c, err := formatCodec(format)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := c.Encode(&b, data); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// writeConfigFile marshals data in the specified format and writes it to
// toPath atomically, preserving existing permissions.
func writeConfigFile(ctx context.Context, toPath string, format string, data any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	out, err := marshalConfig(format, data)
	if err != nil {
		return err
	}
	if err := writeOutput(ctx, toPath, out); err != nil {
		return coreerrs.Wrapf(err, "failed to write config file %s", toPath)
	}
	return nil
}

func writeOutput(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Reject non-regular destinations rather than replacing symlinks or devices.
	mode := os.FileMode(filePermission)
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("output must be a regular file: %w", os.ErrInvalid)
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".nomos-*")
	if err != nil {
		return err
	}
	defer func() { _ = temporary.Close(); _ = os.Remove(temporary.Name()) }()
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}
