// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert_test

import (
	"testing"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands/convert"
)

func BenchmarkFlattenConfig(b *testing.B) {
	data := map[string]any{
		"consumers": map[string]any{
			"user-events":  map[string]any{"workers": 4, "topic": "users"},
			"order-events": map[string]any{"workers": 8, "topic": "orders"},
		},
	}
	conv := convert.NewConfigToEnvConverter()
	b.ReportAllocs()
	for b.Loop() {
		_ = conv.FlattenConfig(data, "")
	}
}
