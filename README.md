# toc-whmcs-mcp

<table>
  <tr>
    <th align="left">Pipeline</th>
    <th align="left">Code</th>
    <th align="left">OpenSpec</th>
    <th align="left">Security</th>
  </tr>
  <tr valign="top">
    <td>
      <a href="../../actions/workflows/ci.yaml"><img src="../../actions/workflows/ci.yaml/badge.svg" alt="CI"></a><br>
      <a href="../../actions/workflows/release-please.yaml"><img src="../../actions/workflows/release-please.yaml/badge.svg" alt="Release"></a><br>
      <a href="../../actions/workflows/registry-drift.yaml"><img src="../../actions/workflows/registry-drift.yaml/badge.svg" alt="Registry drift"></a>
    </td>
    <td>
      <a href="docs/badges/coverage.svg"><img src="docs/badges/coverage.svg" alt="Coverage"></a><br>
      <a href=".tool-versions"><img src="docs/badges/go.svg" alt="Go version"></a><br>
      <a href="internal/registry/actions_gen.go"><img src="docs/badges/actions.svg" alt="WHMCS actions"></a>
    </td>
    <td>
      <a href="openspec/specs/"><img src="docs/badges/specs.svg" alt="Specs"></a><br>
      <a href="openspec/specs/"><img src="docs/badges/requirements.svg" alt="Requirements"></a><br>
      <a href="openspec/changes/"><img src="docs/badges/open-changes.svg" alt="Open changes"></a>
    </td>
    <td>
      <a href="SECURITY.md"><img src="https://img.shields.io/badge/profile-readonly%20by%20default-3fb950?style=flat-square" alt="Read-only by default"></a><br>
      <a href="SECURITY.md"><img src="https://img.shields.io/badge/mutations-confirmation%20required-0a7bbb?style=flat-square" alt="Confirmation required"></a><br>
      <a href="LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-0a7bbb?style=flat-square" alt="AGPL-3.0"></a>
    </td>
  </tr>
</table>

An MCP server that exposes the WHMCS Admin API to LLM agents, for support
triage, billing lookups and provisioning work.

All 162 documented WHMCS actions are reachable. Only 15 to 25 tools are
advertised, depending on the profile, so the tool listing stays small enough for
real MCP clients and for useful tool selection.

Licensed under the [GNU AGPL v3.0](LICENSE). If you run a modified version of
this server and let others interact with it over a network, you must offer them
its source. See [docs/licensing.md](docs/licensing.md).

**[Documentation](docs/)** · [Security model](docs/security-model.md) ·
[Profiles](docs/profiles.md) · [Tool reference](docs/tools.md) ·
[Deployment](docs/deployment.md) · [Troubleshooting](docs/troubleshooting.md) ·
[Licensing](docs/licensing.md) · [Examples](examples/)

---

## Why this exists rather than a thin wrapper

WHMCS is production billing infrastructure. It holds customer personal data,
payment records, support correspondence and credentials for provisioning
servers. A thin one-tool-per-endpoint wrapper around it has three problems that
are not stylistic:

1. The agent can be prompt-injected by the very data the server returns.
   Ticket bodies and client notes are written by customers, and they reach the
   same model that chooses the next tool call.
2. A single API credential authorises everything the WHMCS role permits, and
   the endpoint gives no hint whether an action reads or destroys.
3. Full responses put every customer's personal data into model context and
   provider logs, whether or not the task needed it.

Every control below is enforced inside this process. Nothing is delegated to the
model's good behaviour, to the MCP host's confirmation dialog, or to how
narrowly the WHMCS credential happens to be scoped.

## Security model

**Read-only by default.** With no profile configured the server starts in
`readonly` and advertises no mutating tool at all. Forbidden tools are not
registered, so they are neither listed nor callable.

**Four profiles.** `readonly`, `support`, `billing`, `admin`. Reads are
permitted everywhere; the separation is over who may change what. A support
agent cannot post a payment; a billing agent cannot terminate a service.

**Destructive actions are off even for admin.** Anything irreversible, anything
that moves money, changes provisioning, alters global configuration, or emails a
customer is classified destructive and requires `WHMCS_MCP_ALLOW_DESTRUCTIVE=true`
on top of the profile.

**Two-step confirmation.** A destructive call with no token performs no write. It
returns an impact statement and a server-generated token. The token is an HMAC
over the action and the exact arguments, expires, and works once. A model cannot
mint one, cannot move one to a different target, and cannot execute twice by
retrying. This does not rely on the host's tool-approval dialog, which is
host-dependent and absent in headless deployments.

**Some actions are permanently blocked.** `GetClientPassword`,
`DecryptPassword`, `EncryptPassword`, `CreateSsoToken`, `CreateOAuthCredential`,
`UpdateOAuthCredential` and `ValidateLogin` return or mint credentials. No
profile and no configuration reaches them; enabling one requires a code change
and a review.

**Accurate annotations.** Every tool declares `readOnlyHint`,
`destructiveHint`, `idempotentHint` and `openWorldHint`, derived from the
registry classification rather than set by hand, so a client can trust that
`readOnlyHint: false` really means the tool can change something.

**Data minimisation.** Curated tools project responses onto an allowlist of
declared fields, so a field WHMCS adds in a future version is excluded by
default. Postal address, phone number, tax identifier and admin notes require an
explicit per-call opt-in, and that opt-in is audited. Credentials, card data and
security answers are never returned.

**Untrusted content is labelled.** Customer-authored text comes back inside an
envelope carrying `_untrusted: true`, its origin, and a notice telling the model
it is data rather than instruction. Control characters, invisible characters and
imitation role boundaries are neutralised. This does not make injection
impossible; it makes the boundary explicit, and it is paired with the
confirmation protocol so a successfully injected agent still cannot execute a
mutation on its own.

**Bounded I/O.** Per-request timeouts with context cancellation, a response size
cap, `limit` clamped to a maximum on every collection, and retries only on
read-classified actions, so a 503 cannot cause a payment to be posted twice.

**Runtime response validation.** A 200 status and a JSON content type are not
evidence of a valid API response. An HTML maintenance page or a login redirect
surfaces as a typed error, not as "this client has no invoices".

**Audit trail.** Every invocation gets an operation ID, and confirmation
issuance and consumption share it. Records carry field names and counts, never
values, and go to stderr rather than stdout, which is the MCP channel.

### What this does not do

- It does not replace WHMCS permissions. It narrows what a credential can do and
  never widens it. Provision one credential per profile and scope its WHMCS role
  as well.
- The `whmcs_call_action` escape hatch filters output by denylist rather than
  projecting it onto a declared schema, because it has no per-action schema to
  project onto. Prefer a purpose-built tool where one exists. It is annotated
  destructive because its target is chosen at call time.
- Confirmation tokens are held in memory. A restart invalidates outstanding
  tokens, which is the safe direction, but this design does not yet support
  running replicas behind a load balancer.
- The HTTP transport's bearer token is a coarse gate, not an identity layer.
  Deployments needing per-user attribution should front it with something that
  provides it.

## Quick start

```bash
just setup                 # install the pinned toolchain and dependencies
just setup-env             # create .env from .env.example, then fill it in
set -a; source .env; set +a

just tools                 # see what this configuration would advertise
just healthcheck           # verify connectivity and the credential
just test                  # full suite: no network, no credentials needed
```

Create the API credential in WHMCS under **System Settings > API Credentials**,
and scope its role to the minimum the profile needs.

## Configuration

Every setting is an environment variable prefixed `WHMCS_MCP_`, overridable by a
flag. The server validates its configuration at startup and exits non-zero if
anything is missing or inconsistent, rather than starting and failing every
call.

| Variable | Default | Meaning |
| --- | --- | --- |
| `WHMCS_MCP_WHMCS_URL` | required | WHMCS root URL. Must be https unless loopback. |
| `WHMCS_MCP_API_IDENTIFIER` | required | API credential identifier. |
| `WHMCS_MCP_API_SECRET` | required | API credential secret. |
| `WHMCS_MCP_API_ACCESS_KEY` | unset | Only if the instance requires an access key. |
| `WHMCS_MCP_PROFILE` | `readonly` | `readonly`, `support`, `billing`, `admin`. |
| `WHMCS_MCP_ALLOW_DESTRUCTIVE` | `false` | Enable destructive actions. Confirmation still applies. |
| `WHMCS_MCP_TOOL_ALLOWLIST` | unset | Comma-separated tool names. Narrows the profile only. |
| `WHMCS_MCP_TRANSPORT` | `stdio` | `stdio` or `http`. |
| `WHMCS_MCP_ADDR` | `127.0.0.1:8080` | Bind address for `http`. |
| `WHMCS_MCP_AUTH_TOKEN` | unset | Bearer token. Required for a non-loopback `http` bind. |
| `WHMCS_MCP_REQUEST_TIMEOUT` | `30s` | Per-request deadline. |
| `WHMCS_MCP_MAX_RESPONSE_BYTES` | `8388608` | Response size cap. |
| `WHMCS_MCP_MAX_RETRIES` | `2` | Retries, read-classified actions only. |
| `WHMCS_MCP_DEFAULT_PAGE_SIZE` | `25` | Applied when a call omits `limit`. |
| `WHMCS_MCP_MAX_PAGE_SIZE` | `200` | Ceiling on any requested `limit`. |
| `WHMCS_MCP_CONFIRM_TTL` | `5m` | Confirmation token lifetime. |
| `WHMCS_MCP_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |

### Profiles at a glance

| | readonly | support | billing | admin |
| --- | :---: | :---: | :---: | :---: |
| Read anything | yes | yes | yes | yes |
| Tickets, client records | no | **yes** | client records only | yes |
| Invoices, quotes, orders | no | no | **yes** | yes |
| Provisioning (suspend, terminate) | no | no | no | **yes** |
| Tools advertised | 15 | 19-20 | 16-18 | 20-25 |

Destructive actions within a permitted category still require
`ALLOW_DESTRUCTIVE=true` and a per-call confirmation token. Full matrix, how to
choose, and how to scope the WHMCS credential: **[docs/profiles.md](docs/profiles.md)**.

## Connecting a client

```sh
claude mcp add whmcs \
  -e WHMCS_MCP_WHMCS_URL=https://billing.example.com \
  -e WHMCS_MCP_API_IDENTIFIER=... \
  -e WHMCS_MCP_API_SECRET=... \
  -e WHMCS_MCP_PROFILE=readonly \
  -- /usr/local/bin/toc-whmcs-mcp
```

Ready-to-edit configuration for JSON-config clients, Docker, Compose,
Kubernetes and systemd is in **[examples/](examples/)**. The reasoning behind
each choice is in **[docs/deployment.md](docs/deployment.md)**.

Always reference the container image by digest, never a tag, and verify its
cosign signature before trusting it.

## Tools

Fifteen read tools are always available; ten more appear as the profile and the
destructive setting allow. Run `just tools` against your configuration to see
the exact set.

**Discovery and status**
`whmcs_status`, `whmcs_list_actions`, `whmcs_describe_action`,
`whmcs_call_action`

**Clients**
`whmcs_client_search`, `whmcs_client_get`, `whmcs_client_services`,
`whmcs_client_domains`, `whmcs_client_update`, `whmcs_client_note_add`

**Billing**
`whmcs_invoice_list`, `whmcs_invoice_get`, `whmcs_transaction_list`,
`whmcs_invoice_payment_add`

**Orders**
`whmcs_order_list`, `whmcs_order_accept`

**Support**
`whmcs_ticket_list`, `whmcs_ticket_get`, `whmcs_ticket_reply`,
`whmcs_ticket_note_add`, `whmcs_ticket_update`

**Services**
`whmcs_service_suspend`, `whmcs_service_unsuspend`, `whmcs_service_terminate`

**System**
`whmcs_stats`

Full reference with arguments and worked call sequences:
**[docs/tools.md](docs/tools.md)**.

Anything else goes through the escape hatch:

```
whmcs_list_actions(category: "Domains")   ->  names and summaries
whmcs_describe_action("DomainRenew")      ->  full parameter schema
whmcs_call_action("DomainRenew", {...})   ->  validated, policed, confirmed
```

## The action registry

`internal/registry/actions_gen.go` describes all 162 actions with real parameter
schemas, generated from the vendor's published API reference by `cmd/docgen` and
committed. The runtime never contacts `developers.whmcs.com`.

```bash
just gen         # regenerate; review the diff
just gen-check   # fail if the committed file is stale
```

Safety classification is **not** generated. It lives in
`internal/registry/classification.go` and is maintained by hand, because the
vendor documentation does not say whether an action reads or destroys.
Generation fails outright when the vendor adds an action nobody has classified,
so a WHMCS upgrade cannot quietly introduce an unclassified destructive action.
An unclassified action defaults to `write` at runtime, never `read`.

A scheduled workflow re-runs the check weekly and opens an issue on drift.

## Development

```bash
just              # grouped recipe list
just ci           # everything CI runs, in one command
just test-safety  # policy, confirmation, redaction, injection tests only
just fuzz         # fuzz the parsers that handle attacker-influenced input
just coverage     # HTML coverage report
just lint         # golangci-lint
just sast         # gosec
just audit        # govulncheck
just pin-check    # every GitHub Action must be pinned to a commit SHA
```

Tests run offline against an in-process fake WHMCS
(`internal/whmcs/whmcstest`). No test requires credentials or network access,
and none prints real customer data.

Coverage floors are enforced in CI: 65% overall, and 80% for `internal/policy`,
`internal/confirm`, `internal/redact`, `internal/shape`, `internal/untrusted`
and `internal/whmcs`. The safety packages are held higher on purpose.

Commits follow [Conventional Commits](https://www.conventionalcommits.org/),
enforced by a commit-msg hook (`just setup-hooks`). The version and
`CHANGELOG.md` are generated from them by release-please, so the commit message
is the release note. See [CONTRIBUTING.md](CONTRIBUTING.md).

### Supply chain

The pipeline is part of the attack surface: this binary holds a credential to a
billing system.

- Every GitHub Action is pinned to a commit SHA, not a mutable tag, and
  `just pin-check` fails the build if one is not. Dependabot bumps the pins.
- Workflows default to `permissions: read-all`, with each job requesting only
  what it needs.
- The container base image is pinned by digest.
- CI runs gosec (SAST), govulncheck and Trivy (SCA). A finding fails the build:
  the scanner's exit code is the gate, not the report. The built image is
  scanned, not just the source tree it came from.
- SARIF upload to the Security tab, and the CodeQL workflow, need GitHub
  Advanced Security. This repository is private and does not have it, so the
  uploads are best-effort (`continue-on-error`) and CodeQL is manual-only. They
  start working with no workflow change the day Advanced Security is enabled.
- Releases are signed with cosign keyless signing, ship an SBOM per archive, and
  carry build provenance attestation. Verify an image with:

  ```bash
  cosign verify ghcr.io/theorigamicorporation/toc-whmcs-mcp@sha256:... \
    --certificate-identity-regexp='^https://github.com/theorigamicorporation/toc-whmcs-mcp/' \
    --certificate-oidc-issuer=https://token.actions.githubusercontent.com
  ```

Report vulnerabilities privately: see [SECURITY.md](SECURITY.md).
The full security model, including what an attacker would have to defeat, is in
**[docs/security-model.md](docs/security-model.md)**.

The specification lives in `openspec/`. Changes to behaviour should update it
first:

```bash
just spec-status
just spec-validate
```

## Layout

```
cmd/docgen              generates the action registry from vendor docs
cmd/toc-whmcs-mcp       the server binary
internal/registry       action schemas (generated) and safety classification (hand-maintained)
internal/whmcs          bounded, validated transport to the WHMCS API
internal/policy         profiles, allowlist, permitted action resolution
internal/confirm        prepare/confirm nonce protocol
internal/redact         secret, card and PII denylist redaction
internal/shape          allowlist projection and output schemas
internal/untrusted      customer-content envelope
internal/audit          operation IDs and the audit stream
internal/tools          tool definitions and the single dispatcher
internal/app            assembly, so tests build what ships
openspec/               the specification
```
