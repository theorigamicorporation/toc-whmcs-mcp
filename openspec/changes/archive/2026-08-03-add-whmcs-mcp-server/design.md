## Context

WHMCS exposes ~169 admin actions through a single endpoint: form-encoded POST to
`/includes/api.php`, authenticated by an `identifier`/`secret` pair, with
`responsetype=json`. The uniformity is convenient (one transport, one auth path)
and dangerous (one credential authorises everything the WHMCS role permits, and
the endpoint gives no hint whether an action reads or destroys).

Three constraints shape the design.

First, the tool surface. Advertising 169 tools costs 50-100k tokens of schema
before the agent does anything, degrades tool selection, and exceeds what several
MCP clients accept. But partial coverage means operators fall back to manual
work.

Second, the trust boundary. The server returns customer-authored text: ticket
bodies, client notes, order comments. That text reaches the same model that
decides which tool to call next. Any design that returns it as ordinary trusted
result data hands prompt-injection authors a path to `DeleteClient`.

Third, blast radius. WHMCS holds PII, payment records, service credentials and
server details. An accidental call is a data incident; an accidental
`ModuleTerminate` is an outage a customer notices.

Go was chosen for the single static binary: no runtime, no dependency tree to
audit at deploy time, trivial to ship into a container or onto a jump host.

## Goals / Non-Goals

**Goals:**

- Reach every documented WHMCS action while advertising ~25 tools.
- Enforce every safety property inside the server. The model's cooperation and
  the host's confirmation UI are treated as absent.
- Make the safe path the default path: read-only profile, minimal projection,
  destructive actions off even for `admin`.
- Keep action schemas generated and diffable, so a WHMCS upgrade is a
  regeneration and a code review, not an archaeology exercise.
- Make every mutation attributable after the fact.

**Non-Goals:**

- Not a general WHMCS SDK. The client exists to serve the MCP layer.
- Not a replacement for WHMCS permissions. The server narrows what a credential
  can do; it never widens it. Deployments still scope credentials in WHMCS.
- No caching of WHMCS data. Stale billing data is worse than a slow call.
- No multi-tenant support in this change. One process serves one WHMCS instance
  with one credential.
- No write support for the Products, Affiliates or Project Management categories
  beyond the escape hatch.

## Decisions

### Curated tools plus a three-tool escape hatch

~22 curated tools cover the flows operators actually run (client lookup, invoice
and transaction reads, ticket triage and reply, order and service status). Every
other action is reachable through `whmcs_list_actions` →
`whmcs_describe_action` → `whmcs_call_action`.

Prevents: context exhaustion and degraded tool selection from a 169-tool
listing, without the coverage gap that a curated-only server would have.

The escape hatch is deliberately not a bypass. `whmcs_call_action` runs the same
policy check, confirmation protocol, redaction pass and pagination clamp as a
curated tool, and is annotated as destructive because its target is chosen at
call time. Alternative considered: category tools taking an `action` argument.
Rejected because it produces the same fuzzy dispatch as the escape hatch while
costing 20 tool slots.

### Generated registry as the single source of schema truth

`cmd/docgen` scrapes the WHMCS API index and each action page, parses the request
and response parameter tables, and emits `internal/registry/actions_gen.go`. CI
re-runs it and fails if the committed file differs.

Prevents: hand-maintained schemas drifting from the API, and the "misspelled
parameter silently ignored" failure mode, since validation is driven by the same
table the documentation shows.

Classification (read / write / destructive) is not in the upstream docs, so it
lives in a hand-maintained override table keyed by action name, with a
conservative default. An action absent from the override table is treated as
write, never as read. `docgen` fails if it discovers an action the override table
does not mention, so a WHMCS upgrade cannot quietly introduce an unclassified
destructive action.

### Policy is a filter on tool registration, not a check inside handlers

Profiles resolve at startup to a set of permitted actions. Tools whose action is
not permitted are never registered with the MCP server, so they are neither
listed nor callable. Handlers additionally re-check, because `whmcs_call_action`
resolves its action at call time.

Prevents: the "advertised but forbidden" state, where the model repeatedly
attempts a tool it can never use, and the class of bug where a new handler
forgets its permission check.

### Two-step confirmation with a bound, expiring, single-use nonce

A consequential call with no `confirmation_token` performs no write. It returns
an impact preview and a token. The token is an HMAC over the action name, the
canonicalised arguments, an issue timestamp and a random ID, held in an in-memory
store that marks it consumed on use.

Binding to canonicalised arguments prevents a token issued for service 42 being
replayed against service 43. TTL prevents a token surviving the conversation
that justified it. Single use prevents a retried tool call executing twice.
In-memory storage means a restart invalidates all outstanding tokens, which is
the safe direction.

Alternative considered: relying on the MCP host's tool-approval dialog. Rejected
because it is host-dependent, absent in headless and automated deployments, and
approves a tool rather than a specific target.

### Redaction as a mandatory pipeline stage, not a per-handler habit

Results pass through `project → redact → envelope` before they can be returned.
The return type of the internal handler is a projection, and only the shared
dispatcher can convert it into an MCP result. There is no code path where a
handler returns raw upstream data.

Prevents: the duplicated-serialisation failure mode where one of 60 handlers
forgets to redact. Adding a handler cannot skip the pipeline because the type
system does not offer that shape.

Password-returning actions (`GetClientPassword`, `DecryptPassword`,
`EncryptPassword`) are blocked at the registry level in every profile. There is
no configuration that enables them; enabling them would require a code change and
a review.

### Untrusted content envelope

Customer-authored strings are returned as
`{"_untrusted": true, "origin": "ticket_reply", "content": "..."}` with
delimiters and control characters escaped, rather than as plain string fields.

Prevents: stored prompt injection reading as trusted instruction. This does not
make injection impossible; it makes the boundary explicit and machine-detectable,
and it is paired with the confirmation protocol so that a successfully injected
agent still cannot execute a mutation unilaterally.

### Bounded I/O everywhere

Per-request timeout with context propagation; `io.LimitedReader` capping the
response body; `limit` clamped to a maximum on every collection tool; retries
only on registry-classified read actions and only for transient failures.

Prevents: a stalled WHMCS hanging a tool call indefinitely, a broad query
exhausting memory and model context, and a retried write posting a payment twice.

### Runtime validation of responses

The client decodes into a generic envelope, asserts `result` is present, and
checks the content type and leading bytes before decoding. Each tool then decodes
the payload into its own struct with unknown fields rejected at the projection
step rather than assumed.

Prevents: an HTML login or maintenance page being treated as a successful empty
result, which is how a misconfigured credential turns into "the client has no
invoices".

## Risks / Trade-offs

- **Scraping the upstream documentation is fragile.** A WHMCS site redesign
  breaks `docgen`. Mitigated by committing the generated output, so the build
  never depends on the scrape at runtime, and by a golden-file test over a
  captured fixture page so a parser regression is caught without network access.

- **Classification is hand-maintained.** The read/write/destructive table is
  human judgement and can be wrong. Mitigated by defaulting unknown actions to
  write, failing generation on unclassified actions, and covering the table with
  a test that asserts every known-destructive action is classified destructive.

- **The escape hatch weakens tool-level annotation value.** `whmcs_call_action`
  must be annotated destructive, so hosts cannot distinguish a read through the
  hatch from a delete through it. Accepted: the alternative is either no coverage
  or a 169-tool listing. Operators who want precise annotations should use the
  curated tools, and the `readonly` profile makes the hatch read-only in
  practice.

- **In-memory nonces do not survive restart or scale horizontally.** Accepted for
  this change: one process, one WHMCS instance. A shared store would be required
  before running replicas behind a load balancer, and is called out as a
  follow-up rather than built speculatively.

- **Redaction reduces usefulness.** An agent that cannot see a phone number
  cannot verify a caller. Mitigated by the explicit detail opt-in, which is
  auditable, rather than by loosening the default.

- **Profiles are coarse.** Four profiles will not fit every team. Accepted over a
  general policy language, which would be a larger surface to get wrong; the
  per-tool allowlist provides the narrowing escape valve, and it can only
  subtract.
