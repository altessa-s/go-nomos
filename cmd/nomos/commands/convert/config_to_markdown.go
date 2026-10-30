// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"context"

	"github.com/altessa-s/go-atlas-core/io/files"

	"github.com/altessa-s/go-nomos/internal/conversion"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
)

// ConfigToMarkdownConverter generates a Markdown reference document from a
// YAML or TOML configuration file. The output contains a table of contents
// and per-section tables listing each environment variable, its inferred Go
// type, and its default value. When a Claude API key is configured via
// [ConfigToMarkdownConverter.SetClaudeAPI], an additional "Description"
// column is populated by [ClaudeClient].
type ConfigToMarkdownConverter struct {
	describer conversion.Describer
}

// NewConfigToMarkdownConverter creates a new config to Markdown converter.
func NewConfigToMarkdownConverter() *ConfigToMarkdownConverter {
	return &ConfigToMarkdownConverter{}
}

// SetClaudeAPI enables Claude API integration for generating descriptions.
func (conv *ConfigToMarkdownConverter) SetClaudeAPI(apiKey string) {
	if apiKey == "" {
		conv.describer = nil
		return
	}
	conv.describer = NewClaudeClient(apiKey)
}

// Convert reads a YAML or TOML file at fromPath and writes a Markdown
// environment-variable reference to toPath. YAML input is always
// uncommented before parsing so template files produce complete output.
// #nosec G304 -- CLI tool, path from command-line arguments
func (conv *ConfigToMarkdownConverter) Convert(fromPath, toPath, format string) error {
	return conv.ConvertContext(context.Background(), fromPath, toPath, format)
}

// ConvertContext reads and converts a document under the caller's context.
// #nosec G304 -- CLI input paths are explicitly supplied by the caller.
func (conv *ConfigToMarkdownConverter) ConvertContext(ctx context.Context, fromPath, toPath, format string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Read file
	data, err := files.ReadRegular(fromPath)
	if err != nil {
		return coreerrs.Wrapf(err, "failed to read config file %s", fromPath)
	}

	// Parse based on format
	var configData map[string]any

	if format == formatYAML {
		data = NewConfigToEnvConverter().UncommentYAML(data)
	}
	if err := decodeConfig(format, data, &configData); err != nil {
		return err
	}

	return conv.ConvertDataContext(ctx, configData, toPath)
}

// ConvertData generates a Markdown reference from pre-loaded configData and
// writes it to toPath. If a Claude API key has been set, descriptions are
// generated per-section via a single API call each; an API error for any
// section causes the entire conversion to fail.
func (conv *ConfigToMarkdownConverter) ConvertData(data map[string]any, toPath string) error {
	return conv.ConvertDataContext(context.Background(), data, toPath)
}

// ConvertDataContext renders a reference document under the caller's context.
func (conv *ConfigToMarkdownConverter) ConvertDataContext(ctx context.Context, data map[string]any, toPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	out, err := (&conversion.Markdown{}).Render(ctx, data, conv.describer)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeOutput(ctx, toPath, out)
}
