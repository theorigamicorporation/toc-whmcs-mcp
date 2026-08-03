## 1. Repository scaffold

- [x] 1.1 Add `.tool-versions` pinning `golang 1.26.5`, plus `.gitignore` and `.editorconfig`
- [x] 1.2 Add the proprietary `LICENSE` and a `README.md` documenting the security model
- [x] 1.3 Initialise the Go module and add `github.com/mark3labs/mcp-go`
- [x] 1.4 Add `internal/version` with a build-time-injected version string

## 2. Action registry and code generation

- [x] 2.1 Write `cmd/docgen`: fetch the API index, resolve every action slug and category
- [x] 2.2 Parse request and response parameter tables into typed schema structs
- [x] 2.3 Add the hand-maintained classification table (read / write / destructive) with an unknown-action default of write
- [x] 2.4 Fail generation when an action is missing from the classification table
- [x] 2.5 Emit deterministic, gofmt-clean `internal/registry/actions_gen.go`
- [x] 2.6 Capture a fixture action page and add a golden-file parser test that needs no network
- [x] 2.7 Add registry lookup, category listing and parameter validation helpers
- [x] 2.8 Block password-returning actions at the registry level in all profiles

## 3. WHMCS client

- [x] 3.1 Implement the form-encoded POST transport with credential injection
- [x] 3.2 Add per-request timeout, context cancellation and response size capping
- [x] 3.3 Add runtime response validation: content type, leading bytes, `result` field
- [x] 3.4 Define typed errors: validation, forbidden, upstream unavailable, invalid response, WHMCS API error, too large, timeout
- [x] 3.5 Add bounded retry with jittered backoff, restricted to read-classified actions
- [x] 3.6 Ensure credentials are scrubbed from every error and log path

## 4. Safety layer

- [x] 4.1 `internal/policy`: profiles, destructive opt-in, allowlist narrowing, startup resolution to a permitted action set
- [x] 4.2 `internal/confirm`: HMAC nonce bound to action and canonicalised arguments, with TTL and single-use consumption
- [x] 4.3 `internal/redact`: secret, card and PII redaction with an allowlist-based field projector
- [x] 4.4 `internal/untrusted`: envelope type with delimiter and control-character escaping
- [x] 4.5 `internal/audit`: operation IDs, structured records to stderr or a configured sink, correlated confirmation events

## 5. MCP tool surface

- [x] 5.1 Build the shared dispatcher enforcing policy, confirmation, pagination clamping, projection, redaction and audit
- [x] 5.2 Derive tool annotations from registry classification
- [x] 5.3 Implement curated client, order and service tools
- [x] 5.4 Implement curated billing tools
- [x] 5.5 Implement curated ticket tools with untrusted-content envelopes
- [x] 5.6 Implement `whmcs_list_actions`, `whmcs_describe_action` and `whmcs_call_action`
- [x] 5.7 Implement a `whmcs_status` tool reporting profile, enabled tool count and connectivity
- [x] 5.8 Declare `outputSchema` and return `structuredContent` for every tool
- [x] 5.9 Map every typed error to a stable MCP error code with a retryable flag

## 6. Transports, configuration and entrypoint

- [x] 6.1 Configuration loading from `WHMCS_MCP_` environment variables with flag override
- [x] 6.2 Fail-fast validation with masked configuration logging
- [x] 6.3 Stdio transport, guaranteeing nothing foreign is written to stdout
- [x] 6.4 Streamable HTTP transport with bearer authentication and a loopback-only exception
- [x] 6.5 Graceful shutdown on signal, cancelling in-flight calls

## 7. Tests

- [x] 7.1 `httptest` WHMCS fake covering success, WHMCS error, HTML page, oversized body and stall
- [x] 7.2 Client unit tests: timeout, cancellation, size cap, retry policy, response validation, credential scrubbing
- [x] 7.3 Registry tests: coverage of every indexed action, classification completeness, parameter validation, deprecated-parameter refusal
- [x] 7.4 Policy tests: default read-only, per-profile denial matrix, destructive opt-in, allowlist cannot escalate
- [x] 7.5 Confirmation tests: preview does not write, binding mismatch, expiry, replay consumed
- [x] 7.6 Redaction tests: card, password, security answer, admin note and PII opt-in behaviour
- [x] 7.7 Untrusted envelope tests including delimiter break-out attempts
- [x] 7.8 In-process MCP protocol tests: tool listing size, annotation values, error codes, pagination clamping
- [x] 7.9 An injection scenario test asserting a mutation still requires confirmation

## 8. Tooling, packaging and CI

- [x] 8.1 `justfile` with grouped, colourised recipes and a default listing recipe
- [x] 8.2 `golangci-lint` configuration and a `gofmt` check
- [x] 8.3 Multi-stage `Dockerfile` on a digest-pinned base, with a real connectivity health check
- [x] 8.4 GitHub Actions workflow: build, vet, lint, race tests, generated-registry drift check
- [x] 8.5 Release workflow publishing a versioned image to GHCR
- [x] 8.6 README security model, configuration reference, profile matrix and MCP client setup examples

## 9. Handover

- [x] 9.1 Verify the full suite passes offline
- [ ] 9.2 Create the private repository and push
- [ ] 9.3 Archive this change with `openspec archive`
