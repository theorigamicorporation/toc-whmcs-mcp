# toc-whmcs-mcp — project instructions

MCP server exposing the WHMCS Admin API to LLM agents. Go 1.26, `just` as the
task runner, specification in `openspec/`. Read `README.md` for the security
model before changing anything in the safety layer.

## The governing rule

Safety is enforced **inside this process**. Never delegate a control to the
model's good behaviour, to the MCP host's confirmation dialog, or to how the
WHMCS credential happens to be scoped. A sentence in a tool description is
documentation, not a control.

WHMCS is production billing infrastructure. Assume the model may be
prompt-injected by customer-authored content that this server itself returns
(ticket bodies, client notes, order comments).

## Two files carry most of the risk

**`internal/registry/classification.go`** — the hand-maintained read /
write / destructive / blocked table. When in doubt, classify higher. Destructive
is broader than "deletes a row": it covers anything that moves money, changes
provisioning, alters global configuration, or emails a customer. An action
missing from the table defaults to `write` at runtime and fails generation.

**`internal/tools/dispatch.go`** — the single path every tool call takes.
Policy, confirmation, pagination clamping, projection, redaction and audit are
applied here. A tool definition supplies data only; it never builds an MCP
result itself. Do not add a code path that bypasses `dispatch`.

## Adding a tool

Tools are declarative `tools.Tool` values in `internal/tools/curated.go`. A new
one needs: the WHMCS action name, an argument list, a `shape.Spec` output
projection, `Params`, `Extract`, and a `Preview` if the action is destructive.

- Annotations are derived from the registry classification. Never set them by
  hand, and never declare an action other than the one the tool actually calls.
- The output `shape.Spec` is an allowlist. `Validate` refuses a spec that
  declares a credential field, so that mistake is a boot failure.
- Customer-authored text must be `shape.Untrusted` with an `Origin`.
- Personal data must be `shape.PII`; admin commentary must be `shape.Notes`.
- The advertised surface is capped at 30 tools. Adding one needs an argument
  that the flow is common enough to spend a slot and a slice of every agent's
  context on. Everything else is already reachable through the escape hatch.

Verify with `just tools -profile admin -allow-destructive`.

## Regenerating the registry

`just gen` scrapes the vendor documentation and rewrites
`internal/registry/actions_gen.go`. Never hand-edit that file. Generation fails
when the vendor adds an unclassified action, which is deliberate: a human has to
decide what it does. `just gen-check` fails when the committed file is stale;
a scheduled workflow runs it weekly.

## Testing

Tests run offline against `internal/whmcs/whmcstest`. No test may require a live
WHMCS instance, credentials, or network access, and none may print real customer
data. The protocol-level tests in `internal/app/mcp_test.go` drive a real MCP
client against a server built by `app.Build`, so they exercise what ships.

After touching the safety layer, run `just test-safety`. Before pushing, run
`just ci`.

Fuzz targets cover the two parsers that handle input an attacker influences:
the untrusted-content sanitiser and registry parameter validation. Run
`just fuzz` after changing either. A crash found there is a real finding; the
failing input is written to `testdata/fuzz/` and should be committed as a
regression seed.

Coverage floors are enforced: 65% overall, 80% for `internal/policy`,
`internal/confirm`, `internal/redact`, `internal/shape`, `internal/untrusted`
and `internal/whmcs`.

## Commits and releases

[Conventional Commits](https://www.conventionalcommits.org/), enforced by a
commit-msg hook. release-please derives the version bump and `CHANGELOG.md`
from them, so the commit message is the release note and a wrong type ships a
wrong release note.

Pick the type by what an operator sees, not by which files moved. A change to
the safety classification, a capability profile, or the confirmation protocol is
a behaviour change: `feat:` or `fix:` with a scope, or `feat!:` when something
previously permitted becomes forbidden. Never `chore:` or `refactor:` for those.
`sec:` is available for a security fix that is not otherwise a feature or a bug
fix.

Never tag a release by hand. Merging to `main` updates a release PR; merging
that PR cuts the release.

## Supply chain

Every GitHub Action is pinned to a commit SHA. `just pin-check` fails the build
on a tag reference, so do not add one, and do not "fix" a pin by replacing it
with a tag. Resolve a new pin with:

```sh
gh api repos/OWNER/REPO/git/ref/tags/TAG --jq .object.sha
```

Workflows default to `permissions: read-all`; grant a job only what it needs.
The container base image is pinned by digest for the same reason.

## Conventions

- No AI-sounding boilerplate in commits, PRs, or docs. No em dashes in
  external-facing text.
- Comments explain why, not what. In the safety layer, say what a decision
  prevents.
- Update `openspec/` when behaviour changes; `just spec-validate` must pass.
