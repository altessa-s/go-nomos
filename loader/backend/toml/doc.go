// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package toml provides a TOML backend for the configuration parser.
// Uses github.com/BurntSushi/toml for parsing.
//
// # Usage
//
//	p := loader.New(&toml.Backend{}, loader.WithPath("config.toml"))
//	p.Load(cfg)
package toml
