// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"context"
	"errors"

	"github.com/altessa-s/go-atlas-core/io/files"

	"github.com/altessa-s/go-nomos/internal/conversion"
)

// ErrConflictingKey indicates incompatible assignments to one configuration path.
var ErrConflictingKey = errors.New("conflicting configuration key")

// EnvToConfigConverter reads environment files and writes structured configuration.
type EnvToConfigConverter struct{ options }

// NewEnvToConfigConverter constructs an environment file converter.
func NewEnvToConfigConverter() *EnvToConfigConverter {
	return &EnvToConfigConverter{options: *newOptions()}
}

// SetParseComments selects whether commented assignments are included.
func (conv *EnvToConfigConverter) SetParseComments(parse bool) { conv.parseComments = parse }

// Convert reads fromPath and writes the parsed configuration in format to toPath.
// #nosec G304 -- CLI paths are explicitly supplied by the caller.
func (conv *EnvToConfigConverter) Convert(fromPath, toPath, format string) error {
	return conv.ConvertContext(context.Background(), fromPath, toPath, format)
}

// ConvertContext parses a file with cancellation between lines and before output.
// #nosec G304 -- CLI paths are explicitly supplied by the caller.
func (conv *EnvToConfigConverter) ConvertContext(ctx context.Context, fromPath, toPath, format string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := files.OpenRegular(fromPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	data, err := (&conversion.Parser{}).ParseContext(ctx, f, conv.parseComments)
	if errors.Is(err, conversion.ErrConflictingKey) {
		return errors.Join(ErrConflictingKey, err)
	}
	if err != nil {
		return err
	}
	return writeConfigFile(ctx, toPath, format, data)
}
