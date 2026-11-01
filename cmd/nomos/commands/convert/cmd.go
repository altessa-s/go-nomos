// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/go-atlas-core/io/files"
	"github.com/altessa-s/go-atlas-core/text/strings/intern"

	"github.com/altessa-s/go-nomos/internal/conversion"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"

	coreerrs "github.com/altessa-s/go-atlas-core/errors"
)

// ErrEnvDirectoryInput indicates that a directory was given as the source of
// a conversion from the .env format, which only reads a single file.
var ErrEnvDirectoryInput = errors.New(".env format does not support directory input")

const (
	// File extensions
	extensionYAML = ".yaml"
	extensionYML  = ".yml"
	extensionTOML = ".toml"
	extensionEnv  = ".env"
	extensionMD   = ".md"

	// Format types
	formatYAML = "yaml"
	formatTOML = "toml"
	formatEnv  = "env"
	formatMD   = "md"

	filePermission = 0o644
)

// Command holds the state for a single invocation of "nomos convert".
// It embeds [cobra.Command] and stores the resolved flag values.
type Command struct {
	*cobra.Command
	fromPath      string
	toPath        string
	fromFormat    string
	toFormat      string
	parseComments bool
}

// New returns a cobra.Command that performs configuration file conversion.
// The returned command requires --from and --to flags and supports optional
// --from-format, --to-format, and --parse-comments flags.
func New() *cobra.Command { return NewWithOptions() }

// NewWithOptions constructs a command with generated defaults.
func NewWithOptions(opts ...Option) *cobra.Command {
	cmd := &Command{
		parseComments: newOptions(opts...).parseComments,
		Command: &cobra.Command{
			Use:   "convert",
			Short: "Convert configuration files between formats",
			Long: heredoc.Doc(`
				Convert configuration files between different formats.

				Supported formats:
				- YAML (.yaml, .yml)
				- TOML (.toml)
				- .env (environment variables)
				- Markdown (.md) - documentation tables

				The converter automatically detects formats based on file extensions,
				or you can specify them explicitly using --from-format and --to-format flags.

				Conversion rules:
				- Nested structures use __ (double underscore) as delimiter
				- camelCase field names convert to SCREAMING_SNAKE_CASE
				- Example: GRPC.Interceptors.RealIp -> GRPC__INTERCEPTORS__REAL_IP
			`),
			Example: heredoc.Doc(`
				# Convert YAML to .env
				nomos convert --from config.yaml --to .env

				# Convert .env to YAML
				nomos convert --from .env --to config.yaml

				# Convert TOML to .env
				nomos convert --from config.toml --to .env

				# Convert all files in directory to .env (merges all configs)
				nomos convert --from ./configs/ --to merged.env

				# Parse commented lines (YAML comments -> commented env vars)
				nomos convert --from config.yaml --to .env --parse-comments

				# Parse commented env vars and include them in config
				nomos convert --from .env --to config.yaml --parse-comments

				# Specify format explicitly
				nomos convert --from config.txt --to output.txt --from-format yaml --to-format toml

				# Use short flags
				nomos convert -f config.yml -t production.env
			`),
		},
	}

	cmd.configure()

	return cmd.Command
}

// configure sets up the convert command with flags and run function.
func (c *Command) configure() {
	c.Flags().StringVarP(&c.fromPath, "from", "f", "", heredoc.Doc(`
		Source configuration file or directory path.

		If a directory is specified, all configuration files matching the
		source format will be loaded and merged into a single output.
		Required.
	`))

	c.Flags().StringVarP(&c.toPath, "to", "t", "", heredoc.Doc(`
		Target configuration file path.

		The output format is determined by the file extension unless
		explicitly specified with --to-format.
		Required.
	`))

	c.Flags().StringVar(&c.fromFormat, "from-format", "", heredoc.Doc(`
		Source format: yaml, toml, env, or md.

		If not specified, the format will be auto-detected from the
		file extension. Required when using non-standard extensions.
	`))

	c.Flags().StringVar(&c.toFormat, "to-format", "", heredoc.Doc(`
		Target format: yaml, toml, env, or md.

		If not specified, the format will be auto-detected from the
		file extension. Required when using non-standard extensions.
	`))

	c.Flags().BoolVar(&c.parseComments, "parse-comments", c.parseComments, heredoc.Doc(`
		Parse commented lines in configuration files.

		When converting YAML/TOML to .env, fields with inline comments
		will be output as commented environment variables.
		When converting .env to YAML/TOML, commented lines (starting with #)
		will be included in the output.
	`))

	// Mark flags as required
	// Errors can only occur if the flag doesn't exist, which is a programming error
	// that would be caught during development. Silent ignore is safe here.
	_ = c.MarkFlagRequired("from") //nolint:errcheck
	_ = c.MarkFlagRequired("to")   //nolint:errcheck

	c.RunE = c.run
}

// run executes the convert command.
func (c *Command) run(cmd *cobra.Command, args []string) error {
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	// Validate input paths
	isDir, err := c.validatePaths()
	if err != nil {
		return err
	}

	// Determine source and target formats
	fromFormat := c.fromFormat
	if fromFormat == "" {
		if isDir {
			// For directories, we need explicit format or detect from first file
			fromFormat, err = c.detectDirectoryFormat()
			if err != nil {
				return err
			}
		} else {
			fromFormat = c.detectFormat(c.fromPath)
		}
	}

	toFormat := c.toFormat
	if toFormat == "" {
		toFormat = c.detectFormat(c.toPath)
	}

	// Validate formats
	if err := c.validateFormat(fromFormat, "from"); err != nil {
		return err
	}
	if err := c.validateFormat(toFormat, "to"); err != nil {
		return err
	}

	// Perform conversion
	sourceDesc := c.fromPath
	if isDir {
		sourceDesc = c.fromPath + " (directory)"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Converting %s (%s) -> %s (%s)...\n",
		sourceDesc, fromFormat, c.toPath, toFormat)

	return c.performConversion(cmd.Context(), fromFormat, toFormat, isDir)
}

// validatePaths validates that input and output paths are valid.
// Returns true if source is a directory, false if it's a file.
func (c *Command) validatePaths() (bool, error) {
	// Check if source path exists
	sourceInfo, err := os.Stat(c.fromPath)
	if os.IsNotExist(err) {
		return false, fmt.Errorf("source path does not exist: %s", c.fromPath)
	}
	if err != nil {
		return false, coreerrs.WrapOperation(err, "stat source path")
	}

	// Check if target directory exists
	targetDir := filepath.Dir(c.toPath)
	if targetDir != "." && targetDir != "" {
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			return false, fmt.Errorf("target directory does not exist: %s", targetDir)
		}
	}

	return sourceInfo.IsDir(), nil
}

// detectFormat detects the file format based on extension.
func (c *Command) detectFormat(path string) string {
	ext := intern.LowerString(filepath.Ext(path))

	switch ext {
	case extensionYAML, extensionYML:
		return formatYAML
	case extensionTOML:
		return formatTOML
	case extensionEnv:
		return formatEnv
	case extensionMD:
		return formatMD
	default:
		return ""
	}
}

// detectDirectoryFormat detects the format from the first valid config file in directory.
func (c *Command) detectDirectoryFormat() (string, error) {
	for entry, err := range files.Walk(c.fromPath, files.WithFileTypes(files.FileTypeRegular)) {
		if err != nil {
			return "", coreerrs.WrapOperation(err, "read directory")
		}

		if format := c.detectFormat(entry.Name()); format != "" {
			return format, nil
		}
	}

	return "", fmt.Errorf("no valid config files found in directory: %s", c.fromPath)
}

// validateFormat validates that the format is supported.
func (c *Command) validateFormat(format, formatType string) error {
	if format == "" {
		return fmt.Errorf("%s format could not be detected - use --%s-format flag", formatType, formatType)
	}

	validFormats := []string{formatYAML, formatTOML, formatEnv, formatMD}
	if slices.Contains(validFormats, format) {
		return nil
	}

	return fmt.Errorf("unsupported %s format: %s (supported: %s)",
		formatType, format, strings.Join(validFormats, ", "))
}

// performConversion performs the actual conversion based on formats.
func (c *Command) performConversion(ctx context.Context, fromFormat, toFormat string, isDir bool) error {
	// .env sources are single files; reject a directory before trying to
	// load and merge it, which the .env format does not support.
	if isDir && fromFormat == formatEnv {
		return ErrEnvDirectoryInput
	}

	// If source is a directory, load and merge all files first
	var mergedData map[string]any
	var err error

	if isDir {
		// For markdown conversion, always uncomment YAML files
		uncommentYAML := toFormat == formatMD
		mergedData, err = c.loadAndMergeDirectory(ctx, fromFormat, uncommentYAML)
		if err != nil {
			return err
		}
	}

	fromConfig := isConfigFormat(fromFormat)
	switch {
	case fromConfig && toFormat == formatEnv:
		// Config format (YAML/TOML) to .env
		return c.configToEnv(ctx, fromFormat, isDir, mergedData)
	case fromFormat == formatEnv && isConfigFormat(toFormat):
		// .env to config format (YAML/TOML)
		converter := NewEnvToConfigConverter()
		converter.SetParseComments(c.parseComments)
		return converter.ConvertContext(ctx, c.fromPath, c.toPath, toFormat)
	case fromConfig && isConfigFormat(toFormat):
		// Config format to config format (YAML <-> TOML)
		converter := NewConfigToConfigConverter()
		if isDir {
			return converter.ConvertDataContext(ctx, mergedData, c.toPath, toFormat)
		}
		return converter.ConvertContext(ctx, c.fromPath, c.toPath, fromFormat, toFormat)
	case fromConfig && toFormat == formatMD:
		// Config format (YAML/TOML) to Markdown
		converter := NewConfigToMarkdownConverter()
		if isDir {
			return converter.ConvertDataContext(ctx, mergedData, c.toPath)
		}
		return converter.ConvertContext(ctx, c.fromPath, c.toPath, fromFormat)
	}

	return fmt.Errorf("unsupported conversion: %s to %s", fromFormat, toFormat)
}

// isConfigFormat reports whether format is a structured config format
// (YAML or TOML).
func isConfigFormat(format string) bool {
	return format == formatYAML || format == formatTOML
}

// configToEnv converts a YAML/TOML file, or the merged data of a directory,
// to a .env file.
func (c *Command) configToEnv(ctx context.Context, fromFormat string, isDir bool, mergedData map[string]any) error {
	converter := NewConfigToEnvConverter()
	converter.SetParseComments(c.parseComments)
	if !isDir {
		return converter.ConvertContext(ctx, c.fromPath, c.toPath, fromFormat)
	}
	// When converting from directory with parse-comments, mark all keys as commented
	var commentedKeys map[string]bool
	if c.parseComments {
		commentedKeys = make(map[string]bool)
		markAllAsCommented(mergedData, "", commentedKeys)
	}
	return converter.ConvertDataWithCommentsContext(ctx, mergedData, commentedKeys, c.toPath)
}

// loadAndMergeDirectory loads all config files from a directory and deep-merges
// them into a single map through the conversion service, in lexical order.
// If uncommentYAML is true, commented YAML blocks are uncommented before parsing.
func (c *Command) loadAndMergeDirectory(ctx context.Context, format string, uncommentYAML bool) (map[string]any, error) {
	names, err := c.collectConfigFileNames(format)
	if err != nil {
		return nil, err
	}
	return conversion.Merge(ctx, names, func(ctx context.Context, name string) (map[string]any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		filePath := filepath.Join(c.fromPath, name)
		if !isPathWithinDir(c.fromPath, filePath) {
			return nil, fmt.Errorf("invalid path: %s", name)
		}
		data, err := c.loadConfigFile(filePath, format, uncommentYAML)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", name, err)
		}
		return data, ctx.Err()
	})
}

// collectConfigFileNames returns the base names of regular files in c.fromPath
// whose extension matches the given format.
func (c *Command) collectConfigFileNames(format string) ([]string, error) {
	var names []string
	for entry, err := range files.Walk(c.fromPath,
		files.WithFileTypes(files.FileTypeRegular),
		files.WithExtensions(extensionsForFormat(format)...),
	) {
		if err != nil {
			return nil, coreerrs.WrapOperation(err, "read directory")
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

// extensionsForFormat returns the file extensions associated with a config format.
func extensionsForFormat(format string) []string {
	switch format {
	case formatYAML:
		return []string{extensionYAML, extensionYML}
	case formatTOML:
		return []string{extensionTOML}
	case formatEnv:
		return []string{extensionEnv}
	case formatMD:
		return []string{extensionMD}
	}
	return nil
}

// isPathWithinDir reports whether path is inside dir after cleaning both.
// It uses [filepath.Rel] to avoid prefix-based path traversal bypasses
// (e.g., /tmp/a vs /tmp/ab).
func isPathWithinDir(dir, path string) bool {
	cleanDir := filepath.Clean(dir)
	cleanPath := filepath.Clean(path)

	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// loadConfigFile loads a single config file and returns its data.
// #nosec G304 -- CLI tool, path from command-line arguments
func (c *Command) loadConfigFile(path, format string, uncommentYAML bool) (map[string]any, error) {
	data, err := files.ReadRegular(path)
	if err != nil {
		return nil, coreerrs.WrapOperation(err, "read file")
	}

	var configData map[string]any

	if format == formatYAML && (c.parseComments || uncommentYAML) {
		data = (&yaml3.Backend{}).Uncomment(data)
	}
	if err := decodeConfig(format, data, &configData); err != nil {
		return nil, err
	}
	if format == formatYAML && len(configData) == 0 && (&yaml3.Backend{}).IsFullyCommented(data) {
		if err := decodeConfig(format, (&yaml3.Backend{}).Uncomment(data), &configData); err != nil {
			return nil, err
		}
	}

	return configData, nil
}

func markAllAsCommented(data map[string]any, prefix string, commentedKeys map[string]bool) {
	// Use a converter instance to access the parser package function
	converter := NewConfigToEnvConverter()
	markAllAsCommentedRecursive(data, prefix, commentedKeys, converter)
}

// markAllAsCommentedRecursive is the recursive helper for markAllAsCommented.
func markAllAsCommentedRecursive(data map[string]any, prefix string, commentedKeys map[string]bool, converter *ConfigToEnvConverter) {
	// Flatten the entire config to get all env var names that will be generated
	envVars := converter.FlattenConfig(data, prefix)
	for envKey := range envVars {
		commentedKeys[envKey] = true
	}
}
