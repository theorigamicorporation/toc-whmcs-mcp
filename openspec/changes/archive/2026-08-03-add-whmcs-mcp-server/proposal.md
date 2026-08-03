## Why

We want LLM agents to operate against WHMCS for support triage, billing lookups
and provisioning. Today that means an operator copying data in and out of the
admin area by hand, or a bespoke script per workflow.

Existing open-source WHMCS MCP servers are thin wrappers: they register one tool
per endpoint, return the full WHMCS JSON response verbatim, carry no tool safety
annotations, and gate destructive operations behind nothing more than a "use
with caution" sentence in the tool description. Applied to our production
billing system that is unacceptable. A prompt-injected agent reading a customer
ticket could terminate services, delete clients or post payments, and every
client's personal data would flow into model context and provider logs.

WHMCS is production billing infrastructure holding customer PII, payment
records and server credentials. The security boundary has to be enforced inside
the MCP server itself. Nothing may be delegated to the model's good behaviour,
to the MCP host's confirmation UI, or to how narrowly the WHMCS API credential
happens to be scoped.

## What Changes

A new Go binary, `toc-whmcs-mcp`, serving MCP over stdio and Streamable HTTP.

- **Full API coverage, small tool surface.** All ~169 WHMCS actions are reachable,
  but only ~25 tools are advertised. A curated set covers common flows; a
  three-tool escape hatch (`whmcs_list_actions`, `whmcs_describe_action`,
  `whmcs_call_action`) reaches everything else through a generated schema
  registry. This keeps the tool payload small enough for real MCP clients while
  losing no coverage.
- **Generated registry.** A `docgen` command scrapes the official WHMCS API
  reference and emits Go source containing every action's parameters, types,
  requiredness and deprecation status. Schemas are checked in and regenerable,
  not hand-maintained.
- **Read-only by default.** The server starts in the `readonly` profile. Write
  and destructive capabilities require an explicit profile (`support`,
  `billing`, `admin`) plus, optionally, a per-tool allowlist.
- **Two-step confirmation for consequential operations.** Mutations return an
  impact preview and a short-lived, single-use, server-generated nonce bound to
  the exact call. Execution requires that nonce. A natural-language warning is
  not a control.
- **Accurate MCP annotations.** Every tool declares `readOnlyHint`,
  `destructiveHint`, `idempotentHint` and `openWorldHint` truthfully, so hosts
  can distinguish listing invoices from terminating hosting.
- **Data minimisation and redaction.** Responses are projected to declared
  fields and passed through a redactor that strips passwords, card data,
  security answers and admin-only notes before they reach the model.
- **Untrusted content is labelled.** Customer-authored text (ticket bodies and
  replies, client notes, email bodies, order notes) is wrapped in an explicit
  untrusted-data envelope so the agent does not read it as instructions.
- **Bounded I/O.** Request timeouts, context cancellation, response size caps,
  bounded pagination with enforced maximums, and retries only on idempotent
  reads.
- **Runtime response validation.** WHMCS responses are validated at runtime, not
  type-asserted. HTML error pages and malformed JSON surface as typed MCP errors.
- **Structured results.** Tools declare `outputSchema` and return
  `structuredContent`, not pretty-printed JSON in a text block.
- **Real tests.** Mocked unit tests plus in-process MCP protocol tests. No test
  requires a live WHMCS instance, and CI runs more than a type check.

## Capabilities

### New Capabilities

- `whmcs-api-client`: authenticated, bounded, validated transport to the WHMCS
  Admin API, including the generated action registry.
- `mcp-tool-surface`: the advertised MCP tools, their annotations, schemas and
  the generic action escape hatch.
- `access-control`: profiles, per-tool allowlists, and the prepare/confirm nonce
  protocol gating consequential operations.
- `data-protection`: redaction, field projection, untrusted-content tagging and
  audit logging.
- `operations`: configuration, transports, build, container image and CI.

## Impact

- New repository `theorigamicorporation/toc-whmcs-mcp` (private, proprietary).
- New dependency: `github.com/mark3labs/mcp-go`.
- Requires a WHMCS API credential (identifier/secret) per deployment. Deployments
  are expected to provision one credential per profile, scoped in WHMCS itself,
  so the server's profile and the credential's WHMCS permissions reinforce each
  other.
- `docgen` fetches from `developers.whmcs.com` at generation time only. The
  runtime binary never touches it.
