# Troubleshooting

Every failure comes back as an MCP tool result with `isError` set and a
structured payload:

```json
{
  "error": true,
  "code": "forbidden",
  "message": "action DeleteClient modifies data and the server is running in the readonly profile",
  "retryable": false,
  "details": { "profile": "readonly", "classification": "destructive" }
}
```

The code is stable and safe to branch on. `retryable` says whether an identical
retry could possibly help.

## Error codes

| Code | Retryable | What happened | What to do |
| --- | :---: | --- | --- |
| `invalid_params` | no | Missing, unknown, misspelled, out-of-range or deprecated argument | Read `details.accepted_parameters`, or call `whmcs_describe_action` |
| `unknown_action` | no | No such WHMCS action | `whmcs_list_actions` to find the right name |
| `forbidden` | no | The profile, allowlist, or a permanent block forbids it | Check `details.profile`; see [profiles.md](profiles.md) |
| `confirmation_required` | no | Destructive call with no token | Show the impact to a human, then repeat with the token |
| `confirmation_mismatch` | no | Token was issued for a different action or arguments | Request a fresh preview and confirm that one |
| `confirmation_expired` | no | Token TTL elapsed | Request a fresh preview |
| `confirmation_consumed` | no | Token already used | The operation already ran exactly once. Verify before retrying |
| `whmcs_error` | no | WHMCS itself reported an error | The message is the vendor's; usually bad IDs or WHMCS-side permissions |
| `invalid_response` | no | Not a valid API response: HTML page, malformed JSON, no `result` | Check the base URL and whether WHMCS is in maintenance mode |
| `response_too_large` | no | Response exceeded the size cap and was discarded | Narrow the query: smaller `limit`, more specific filters |
| `upstream_unavailable` | **yes** | Could not reach WHMCS, or 5xx after retries | Check connectivity; retry is reasonable |
| `timeout` | **yes** | WHMCS did not respond before the deadline | Retry, or raise `WHMCS_MCP_REQUEST_TIMEOUT` |
| `cancelled` | no | The caller cancelled mid-flight | Nothing; the request was aborted |
| `internal` | no | A defect | Report it: [SECURITY.md](../SECURITY.md) or an issue |

## Common situations

### The server will not start

Configuration is validated at startup and the process exits non-zero rather than
serving tools it cannot execute. The message names every problem at once:

```
invalid configuration:
  - WHMCS_MCP_API_SECRET is required
  - default page size cannot exceed max page size
```

Other startup refusals:

- `WHMCS base URL must use https` — credentials travel in the body of every
  request. Plaintext to a remote host would put them on the wire. Loopback is
  exempt.
- `unknown profile "x"; valid profiles are: readonly, support, billing, admin`
- `the http transport is bound to 0.0.0.0:8080 with no WHMCS_MCP_AUTH_TOKEN set`
  — an unauthenticated HTTP transport on a routable address is a remote control
  plane for a billing system. Set a token or bind to loopback.

### A tool the docs mention is not listed

The profile does not permit it, so it is not registered. Check with:

```sh
just tools -profile support -allow-destructive
```

If you named it in `WHMCS_MCP_TOOL_ALLOWLIST` and it still does not appear, the
allowlist can only subtract; the startup log will have warned about the ignored
entry.

### Everything returns `forbidden`

Either the profile is `readonly` (the default) and you are attempting writes, or
destructive actions are not enabled. `whmcs_status` reports both.

If reads are also forbidden, the action may be permanently blocked — the seven
credential-returning actions are unreachable in every configuration, by design.

### `invalid_response` mentioning an HTML page

WHMCS returned a web page with HTTP 200. Usually:

- The base URL points somewhere other than the WHMCS root. It should be
  `https://billing.example.com`, not `.../includes/api.php` (though that form is
  tolerated) and not the client area login.
- WHMCS is in maintenance mode.
- A WAF or proxy is intercepting the request.

The server refuses to decode it rather than reporting an empty result, because
"this client has no invoices" is a dangerous thing to tell someone who is about
to act on it.

### `forbidden` with "WHMCS rejected the API credential"

HTTP 401/403 from WHMCS itself. Check the identifier and secret, and check the
API IP allowlist in **System Settings → API Credentials**: WHMCS can restrict
API access by source IP, and a containerised deployment's egress IP is often not
what you expect.

### Payments or terminations appear to have run twice

They should not be able to. Confirmation tokens are single use, and write
actions are never retried automatically. Check the audit stream:

```sh
grep '"action":"AddInvoicePayment"' audit.log
```

`confirmation.issued` and `confirmation.consumed` share the operation ID of the
executed mutation. Two `tool.finish` records with `outcome: success` for the
same target and different operation IDs means two deliberate calls, each with
its own token.

### Results are missing fields you expect

Three possibilities, in order of likelihood:

1. The field is personal data or an admin note, and needs
   `include_personal_details` or `include_notes`.
2. The field is a credential, and is never returned. Its key will be present
   with `[withheld by policy]`.
3. The field is not declared in the tool's output schema. Curated tools project
   onto an allowlist, so anything undeclared is dropped. Use
   `whmcs_call_action` to see the wider response, or add the field to the tool's
   spec and its output schema.

### Only part of a collection came back

Look at `limit_clamped`, `total` and `has_more`. `limit` is clamped to
`WHMCS_MCP_MAX_PAGE_SIZE`. Page with `offset`, or narrow the query.

## Reading the audit stream

Audit goes to stderr, one JSON object per line, never to stdout.

```sh
toc-whmcs-mcp 2> audit.log
```

```json
{"msg":"tool.start","op":"op-3k9x","tool":"whmcs_service_terminate","action":"ModuleTerminate","class":"destructive","profile":"admin","arg_keys":["service_id"],"arg_count":1,"pii_requested":false}
{"msg":"confirmation.issued","op":"op-3k9x","action":"ModuleTerminate","expires_at":"..."}
{"msg":"tool.finish","op":"op-3k9x","outcome":"success","code":"","duration":412000000}
```

Useful queries:

| Question | Query |
| --- | --- |
| What mutations ran? | `grep '"class":"destructive"' audit.log \| grep tool.finish` |
| What authorised one? | `grep '"op":"op-3k9x"' audit.log` |
| Who read full contact records? | `grep '"pii_requested":true' audit.log` |
| What was refused? | `grep policy.denied audit.log` |
| Any forged or replayed tokens? | `grep confirmation.rejected audit.log` |

`confirmation.rejected` is the line worth alerting on. It means something
presented a token that was forged, replayed, or aimed at a different target.

Records carry argument **names**, never values, so the stream is safe to ship to
a log aggregator.

## Getting more detail

```sh
WHMCS_MCP_LOG_LEVEL=debug
```

If you need to reproduce something without touching production, the test suite
runs against an in-process fake (`internal/whmcs/whmcstest`) with no credentials
and no network. Adding a case there is usually faster than reproducing against a
live instance, and it cannot leak customer data.
