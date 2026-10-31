// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secrets_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/altessa-s/go-nomos/loader/secrets"
)

func BenchmarkHasSecrets_WithSecret(b *testing.B) {
	content := "password=$__secret{app:password}"
	for b.Loop() {
		_ = secrets.HasSecrets(content)
	}
}

func BenchmarkHasSecrets_NoSecret(b *testing.B) {
	content := "password=plaintext"
	for b.Loop() {
		_ = secrets.HasSecrets(content)
	}
}

func BenchmarkExpander_Expand(b *testing.B) {
	mgr := newMockManager(map[string]string{"app:password": "s3cret"})
	expander := secrets.New(mgr)
	ctx := b.Context()
	content := "pass=$__secret{app:password}"
	b.ResetTimer()
	for b.Loop() {
		_, _ = expander.Expand(ctx, content)
	}
}

func BenchmarkExpandString_NoSecrets(b *testing.B) {
	mgr := newMockManager(map[string]string{})
	ctx := b.Context()
	content := "plain text with no secrets"
	b.ResetTimer()
	for b.Loop() {
		_, _ = secrets.ExpandString(ctx, content, mgr)
	}
}

// longContent returns a document of n lines, every tenth carrying a
// placeholder when withSecrets is set.
func longContent(n int, withSecrets bool) string {
	var sb strings.Builder
	for i := range n {
		sb.WriteString("key" + strconv.Itoa(i) + ": ")
		if withSecrets && i%10 == 0 {
			sb.WriteString("$__secret{app:k" + strconv.Itoa(i) + "}\n")
			continue
		}
		sb.WriteString("plain value with a $ sign and {braces}\n")
	}
	return sb.String()
}

func BenchmarkHasSecrets_Long(b *testing.B) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "none", content: longContent(2000, false)},
		{name: "some", content: longContent(2000, true)},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.content)))
			for b.Loop() {
				_ = secrets.HasSecrets(tc.content)
			}
		})
	}
}

func BenchmarkReferences_Long(b *testing.B) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "none", content: longContent(2000, false)},
		{name: "some", content: longContent(2000, true)},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.content)))
			for b.Loop() {
				_ = secrets.References(tc.content)
			}
		})
	}
}

func BenchmarkExpander_Expand_Long(b *testing.B) {
	values := map[string]string{}
	for i := 0; i < 2000; i += 10 {
		values["app:k"+strconv.Itoa(i)] = "v"
	}
	expander := secrets.New(newMockManager(values))
	content := longContent(2000, true)
	ctx := b.Context()
	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	for b.Loop() {
		if _, err := expander.Expand(ctx, content); err != nil {
			b.Fatal(err)
		}
	}
}
