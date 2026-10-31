// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package main provides nomos, a CLI tool for converting configuration
// files between formats.
//
// Supported formats are YAML, TOML, .env, and Markdown. The tool
// auto-detects formats from file extensions and can be overridden with
// explicit --from-format / --to-format flags.
//
// When the source path is a directory, all matching config files are loaded
// concurrently and deep-merged in lexical filename order into a single output.
//
// Commented-out YAML blocks are handled specially: fully-commented template
// files are auto-uncommented so their values can be extracted, while
// mixed files preserve only real (uncommented) content unless
// --parse-comments is set.
//
// # Usage
//
// Single-file conversion:
//
//	nomos convert --from config.yaml --to .env
//
// Directory merge:
//
//	nomos convert --from ./configs/ --to merged.env
package main
