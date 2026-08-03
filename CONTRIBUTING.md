# Contributing to toc-whmcs-mcp

Internal project. Access is on a need-to-know basis; see [LICENSE](LICENSE).

Read [README.md](README.md) for the security model and
[CLAUDE.md](CLAUDE.md) for the working rules before changing anything under
`internal/`.

## Getting started

```sh
git clone git@github.com:theorigamicorporation/toc-whmcs-mcp.git
cd toc-whmcs-mcp
just setup          # toolchain from .tool-versions, then go mod download
just setup-tools    # golangci-lint, gosec, actionlint
just test           # offline: no WHMCS instance or credentials needed
```

Requirements: Go 1.26 (pinned in `.tool-versions`, use
[asdf](https://asdf-vm.com/)), [just](https://just.systems/), and
[pre-commit](https://pre-commit.com/#install).

Install the git hooks after cloning:

```sh
pre-commit install --hook-type commit-msg
```

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/), enforced by the
commit-msg hook. The version number and `CHANGELOG.md` are generated from them
by release-please, so the message is the release note.

```
<type>[optional scope]: <description>

Types: feat, fix, sec, docs, style, refactor, perf, test, build, ci, chore, revert
```

`fix:` bumps the patch version, `feat:` the minor, and `feat!:` or a
`BREAKING CHANGE:` footer the major.

Pick the type by what an operator sees, not by which files moved. A change to
the safety classification, a capability profile, or the confirmation protocol is
a behaviour change: use `feat:` or `fix:` with a scope, or `feat!:` when an
operation that used to be permitted becomes forbidden. Do not hide those under
`chore:` or `refactor:` — they are exactly what someone reads the changelog for.

Use `sec:` for a security fix that is not otherwise a user-visible feature or
bug fix; it gets its own changelog section.

No AI-sounding boilerplate anywhere externally visible, and no em dashes in
commit messages, PR descriptions, or docs.

## Making a change

1. Branch from `main`.
2. If behaviour changes, update the specification first. `openspec/specs/` is
   the source of truth, not a description written afterwards.
3. Make the change. New behaviour needs tests.
4. `just ci` must pass. After touching the safety layer, also run
   `just test-safety`.
5. Open a PR against `main`. All checks must pass and CODEOWNERS must approve.

## Running the full pipeline locally

```sh
just ci             # fmt check, vet, race tests with shuffle, coverage gate
just lint           # golangci-lint
just actionlint     # workflow linting
just sast           # gosec
just audit          # govulncheck
just fuzz           # fuzz targets, 30s each
```

## Where the risk is

Two files carry most of it. Changes to either need a careful review, not a
rubber stamp.

**`internal/registry/classification.go`** decides whether an action is a read, a
write, something destructive, or permanently blocked. It drives the MCP
annotations, which profile permits the action, and whether the confirmation
protocol applies. When in doubt, classify higher: destructive covers anything
that moves money, changes provisioning, alters global configuration, or emails a
customer. An action absent from the table defaults to `write`, never `read`, and
fails code generation.

**`internal/tools/dispatch.go`** is the single path every tool call takes. Do
not add a code path around it.

## Tests

Tests run offline against `internal/whmcs/whmcstest`. No test may require a live
WHMCS instance, credentials, or network access, and none may print real customer
data. If you find yourself wanting to point a test at a real instance, the fake
needs extending instead.

Coverage gates in CI: 65% overall, and 80% for `internal/policy`,
`internal/confirm`, `internal/redact`, `internal/shape`, `internal/untrusted`
and `internal/whmcs`. The safety packages are held higher on purpose.

Fuzz targets live next to the code they cover, on the parsers that handle input
an attacker influences:

```sh
go test -fuzz=FuzzSanitise -fuzztime=60s ./internal/untrusted/
go test -fuzz=FuzzValidate -fuzztime=60s ./internal/registry/
```

## Regenerating the action registry

```sh
just gen         # scrape the vendor docs, rewrite internal/registry/actions_gen.go
just gen-check   # fail if the committed file is stale
```

Never hand-edit `actions_gen.go`. Generation fails when the vendor adds an
action nobody has classified; that is deliberate, and the fix is to classify it,
not to bypass the check.

## Releasing

Merging to `main` updates a release PR. Merging that PR tags the release and
triggers the signed build. Nobody tags by hand in the normal case.
