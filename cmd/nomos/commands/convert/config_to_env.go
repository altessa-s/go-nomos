// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"bytes"
	"context"
	"strings"

	"github.com/altessa-s/go-atlas-core/io/files"

	"github.com/altessa-s/go-nomos/internal/conversion"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
	corestrings "github.com/altessa-s/go-atlas-core/text/strings"
)

// ConfigToEnvConverter flattens YAML or TOML configuration trees into .env
// KEY=VALUE lines. Keys are built by joining SCREAMING_SNAKE_CASE field names
// with __ as the section delimiter. Output is grouped by top-level section
// with visual separators.
type ConfigToEnvConverter struct {
	options
}

// NewConfigToEnvConverter creates a new config to .env converter.
func NewConfigToEnvConverter() *ConfigToEnvConverter {
	return &ConfigToEnvConverter{options: *newOptions()}
}

// SetParseComments enables or disables parsing of commented configuration values.
// When enabled, configuration values will be output as commented environment variables.
func (conv *ConfigToEnvConverter) SetParseComments(parse bool) {
	conv.parseComments = parse
}

// UncommentYAML strips leading # from lines that look like YAML key-value pairs
// or list items, leaving prose comments intact. Values starting with YAML-special
// characters (*, &, !, @, `) are auto-quoted to prevent parse errors.
func (conv *ConfigToEnvConverter) UncommentYAML(data []byte) []byte {
	return (&yaml3.Backend{}).Uncomment(data)
}

// Convert reads a YAML or TOML file at fromPath and writes the flattened
// .env output to toPath. When parseComments is enabled and format is "yaml",
// inline and block comments are tracked and emitted as commented env lines.
// #nosec G304 -- CLI tool, path from command-line arguments
func (conv *ConfigToEnvConverter) Convert(fromPath, toPath, format string) error {
	return conv.ConvertContext(context.Background(), fromPath, toPath, format)
}

// ConvertContext reads and converts a document with caller cancellation.
// #nosec G304 -- CLI paths are explicitly supplied by the caller.
func (conv *ConfigToEnvConverter) ConvertContext(ctx context.Context, fromPath, toPath, format string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Read file
	data, err := files.ReadRegular(fromPath)
	if err != nil {
		return coreerrs.Wrapf(err, "failed to read config file %s", fromPath)
	}

	// Parse based on format and extract comments if needed
	var configData map[string]any
	var commentedKeys map[string]bool

	if conv.parseComments && format == formatYAML {
		configData, commentedKeys, err = conv.parseYAMLWithComments(data)
		if err != nil {
			return err
		}
	} else {
		if err = decodeConfig(format, data, &configData); err != nil {
			return err
		}
	}

	return conv.ConvertDataWithCommentsContext(ctx, configData, commentedKeys, toPath)
}

// ConvertData flattens pre-loaded configData and writes it to toPath in .env format.
// No comment tracking is performed; all lines are emitted as active variables.
func (conv *ConfigToEnvConverter) ConvertData(configData map[string]any, toPath string) error {
	return conv.ConvertDataWithComments(configData, nil, toPath)
}

// ConvertDataWithComments flattens configData into .env format and writes it
// to toPath. Keys present in commentedKeys are prefixed with # in the output.
// Pass a nil commentedKeys map to emit all variables as active.
func (conv *ConfigToEnvConverter) ConvertDataWithComments(data map[string]any, comments map[string]bool, toPath string) error {
	return conv.ConvertDataWithCommentsContext(context.Background(), data, comments, toPath)
}

// ConvertDataWithCommentsContext renders data, checking cancellation before output.
func (conv *ConfigToEnvConverter) ConvertDataWithCommentsContext(ctx context.Context, data map[string]any, comments map[string]bool, toPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	out := (&conversion.Env{}).Render(data, comments, conv.parseComments)
	return writeOutput(ctx, toPath, out)
}

// FlattenConfig flattens a tree using the converter's environment naming rules.
func (conv *ConfigToEnvConverter) FlattenConfig(data map[string]any, prefix string) map[string]string {
	return (&conversion.Env{}).FlattenConfig(data, prefix)
}

func (conv *ConfigToEnvConverter) parseYAMLWithComments(data []byte) (map[string]any, map[string]bool, error) {
	b := &yaml3.Backend{}
	content := b.Uncomment(data)
	var tree map[string]any
	if err := b.Decode(bytes.NewReader(content), &tree); err != nil {
		return nil, nil, err
	}
	comments := make(map[string]bool)
	if b.IsFullyCommented(data) {
		markAllAsCommented(tree, "", comments)
		return tree, comments, nil
	}
	paths, err := b.CommentPaths(content)
	if err != nil {
		return nil, nil, err
	}
	for _, path := range paths {
		for i := range path {
			path[i] = corestrings.ToScreamingSnakeCase(path[i])
		}
		comments[strings.Join(path, "__")] = true
	}
	return tree, comments, nil
}
