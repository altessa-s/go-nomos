# GitHub Actions scripts

Scripts used by the repository workflows. Commit validation follows the shared go-atlas-core/go-atlas-net conventions, with go-nomos package scopes.

## validate-commits.sh

Validates `type[(scope)]: description` and checks optional scopes against [`commit_scopes.txt`](../../commit_scopes.txt). Supported types are `feat`,
`fix`, `docs`, `style`, `refactor`, `test`, `chore`, `ci`, `perf`, and `build`. Descriptions must start with a lowercase letter. Merge, revert,
auto-merge, and automated `build(deps)` / `chore(deps)` messages are skipped.

### Usage

```bash
.github/scripts/validate-commits.sh
```

Locally, the script fetches `origin/develop` and checks commits ahead of it, falling back to recent history. In pull requests it compares against
`GITHUB_BASE_REF`; push events check the ten most recent commits. No commit is created or rewritten.

### Examples

```text
feat(loader/backend): add a JSON backend
fix(loader/secrets): preserve quoted placeholders
test(internal/conversion): cover environment round trips
docs: update installation instructions
build(deps): update dependencies
```

### Exit status

| Status | Meaning |
|--------|---------|
| `0` | All checked messages pass, no commits were found, or several commits failed outside a pull request (legacy warning mode). |
| `1` | Invalid messages in a pull request, an invalid single commit, or a validation setup failure. |

The [local commit hook](../../.githooks/commit-msg) uses the same scope list. It warns on an uppercase description while CI rejects it. Install the hook
as described in [CONTRIBUTING.md](../../CONTRIBUTING.md); running the validator does not install hooks.
