// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conversion_test

import (
	"testing"

	"github.com/altessa-s/go-nomos/internal/conversion"
)

func BenchmarkRenderEnv(b *testing.B) {
	data := map[string]any{"server": map[string]any{"port": 8080, "host": "localhost"}}
	for b.Loop() {
		_ = (&conversion.Env{}).Render(data, nil, false)
	}
}
