// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"context"

	"github.com/altessa-s/go-atlas-core/io/files"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
)

// ConfigToConfigConverter converts between structured configuration formats
// (YAML and TOML) without flattening to environment variables. The data model
// is preserved as-is; only the serialization format changes.
type ConfigToConfigConverter struct{}

// NewConfigToConfigConverter creates a new config to config converter.
func NewConfigToConfigConverter() *ConfigToConfigConverter {
	return &ConfigToConfigConverter{}
}

// Convert reads fromPath in fromFormat, then writes it to toPath in toFormat.
// Supported format values are "yaml" and "toml".
// #nosec G304 -- CLI tool, path from command-line arguments
func (conv *ConfigToConfigConverter) Convert(fromPath, toPath, fromFormat, toFormat string) error {
	return conv.ConvertContext(context.Background(), fromPath, toPath, fromFormat, toFormat)
}

// ConvertContext converts a file with caller cancellation between I/O phases.
// #nosec G304 -- CLI paths are explicitly supplied by the caller.
func (conv *ConfigToConfigConverter) ConvertContext(ctx context.Context, fromPath, toPath, fromFormat, toFormat string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Read source file
	data, err := files.ReadRegular(fromPath)
	if err != nil {
		return coreerrs.Wrapf(err, "failed to read config file %s", fromPath)
	}

	// Parse source format
	var configData map[string]any

	if err := decodeConfig(fromFormat, data, &configData); err != nil {
		return err
	}

	return conv.ConvertDataContext(ctx, configData, toPath, toFormat)
}

// ConvertData writes pre-loaded configData to toPath in toFormat.
// This is used by the directory-merge path where files have already been
// parsed and merged.
func (conv *ConfigToConfigConverter) ConvertData(configData map[string]any, toPath, toFormat string) error {
	return conv.ConvertDataContext(context.Background(), configData, toPath, toFormat)
}

// ConvertDataContext serializes a tree with cancellation before output.
func (conv *ConfigToConfigConverter) ConvertDataContext(ctx context.Context, data map[string]any, toPath, toFormat string) error {
	return writeConfigFile(ctx, toPath, toFormat, data)
}
