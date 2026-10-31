// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secrets_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/altessa-s/go-nomos/loader/secrets"
)

// injected is what the mock manager returns for one key: a value that is itself
// a placeholder. Expansion must not look at it again.
const injected = "$__secret{other:key}"

// FuzzExpansionIsNotRecursive is the secret-injection oracle.
//
// Placeholders are resolved in configuration text that a deploy tool assembles,
// and the values come from a secrets backend. If a resolved value were scanned
// for placeholders in turn, a secret whose *content* names another key would
// pull that second secret into the configuration — an escalation driven by
// whoever can write the first secret's value, not by whoever writes the config.
//
// The property is about provenance, not output text: every lookup the resolver
// sees comes from a placeholder in the input, in input order, exactly once, and
// a value the manager returned appears in the output verbatim. Expansion is one
// pass, not a fixpoint. The input may legitimately name "other:key" itself, so
// the output alone cannot tell a rescan from a direct reference.
func FuzzExpansionIsNotRecursive(f *testing.F) {
	f.Add("$__secret{app:injector}")
	f.Add("prefix $__secret{app:injector} suffix")
	f.Add("$__secret{app:injector}$__secret{app:injector}")
	f.Add("plain")
	f.Add("")
	f.Add(injected)

	mgr := newMockManager(map[string]string{
		"app:injector": injected,
		"other:key":    "resolved-directly",
	})

	f.Fuzz(func(t *testing.T, content string) {
		var lookups []string
		expander := secrets.New(secrets.ResolverFunc(func(ctx context.Context, ref string) (string, error) {
			lookups = append(lookups, ref)
			return mgr.Resolve(ctx, ref)
		}))

		out, err := expander.Expand(t.Context(), content)
		if err != nil {
			return
		}

		var want []string
		for _, m := range placeholderRefs.FindAllStringSubmatch(content, -1) {
			want = append(want, m[1]+":"+m[2])
		}
		require.Equal(t, want, lookups,
			"the resolver saw a lookup that no input placeholder asked for:\n%s", out)

		// The resolved value is data, so it survives verbatim — including the
		// braces that would have made it a placeholder had it been rescanned.
		injections := 0
		for _, ref := range lookups {
			if ref == "app:injector" {
				injections++
			}
		}
		require.GreaterOrEqual(t, strings.Count(out, injected), injections,
			"a resolved value was altered or rescanned:\n%s", out)
	})
}

// placeholderRefs is the test's own copy of the placeholder grammar, used as
// the oracle for which lookups an input may cause.
var placeholderRefs = regexp.MustCompile(`\$__secret\{([^:}]+):([^}]+)\}`)

// FuzzExpandResolvesEveryPlaceholderItRecognizes pins the relationship between
// the detector and the expander.
//
// HasSecrets is what a loader uses to decide whether expansion is needed at
// all, so a string it reports as clean but the expander would have changed
// means a placeholder ships to production unresolved — a literal
// "$__secret{...}" in a connection string, failing at the far end with no
// mention of secrets anywhere.
func FuzzExpandResolvesEveryPlaceholderItRecognizes(f *testing.F) {
	f.Add("$__secret{ns:key}")
	f.Add("$__secret{no_colon}")
	f.Add("$$__secret{ns:key}")
	f.Add("$__secret{a:b}$__secret{c:d}")
	f.Add("${not_a_secret}")
	f.Add("$__secret{:}")
	f.Add("$__secret{a:}")
	f.Add("")

	mgr := newMockManager(map[string]string{
		"ns:key": "value",
		"a:b":    "val1",
		"c:d":    "val2",
	})
	expander := secrets.New(mgr)

	f.Fuzz(func(t *testing.T, content string) {
		out, err := expander.Expand(t.Context(), content)
		if err != nil {
			return
		}

		if !secrets.HasSecrets(content) {
			require.Equal(t, content, out,
				"the expander changed a string HasSecrets called clean: %q", content)
			return
		}

		require.False(t, secrets.HasSecrets(out),
			"expansion left a placeholder the detector still recognizes:\n%s", out)
	})
}
