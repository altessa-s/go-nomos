// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package yaml3 provides a YAML backend for the configuration parser.
// Uses gopkg.in/yaml.v3 for parsing.
//
// # Usage
//
//	p := loader.New(&yaml3.Backend{}, loader.WithPath("config.yaml"))
//	p.Load(cfg)
package yaml3
