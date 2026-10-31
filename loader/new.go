// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"github.com/altessa-s/go-nomos/loader/backend"
	"github.com/altessa-s/go-nomos/loader/backend/yaml3"
)

// New constructs a loader. A nil backend selects YAML. Options configure sources
// and binding; each LoadContext creates independent working state.
func New(b backend.Backend, opts ...Option) *Config {
	if b == nil {
		b = &yaml3.Backend{}
	}
	return &Config{options: newOptions(opts...), backend: b, loadSlot: make(chan struct{}, 1)}
}
