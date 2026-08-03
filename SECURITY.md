# Security Policy

This server holds an API credential to The Origami Corporation's WHMCS billing
system and mediates an LLM agent's access to customer data. Treat findings here
as production security issues.

## Supported versions

| Version | Supported |
| ------- | --------- |
| latest release | Yes |
| anything older | No |

There is no backport policy. Fixes land in a new release.

## Reporting a vulnerability

**Do not open a public issue, and do not describe the issue in a pull request
title or commit message.**

Report privately via
[GitHub Security Advisories](https://github.com/theorigamicorporation/toc-whmcs-mcp/security/advisories/new),
or by email to security@theorigamicorporation.com.

Include:

- What the issue is and what it lets an attacker do.
- Steps to reproduce, ideally against a test WHMCS instance rather than
  production.
- Affected version or commit.
- A suggested fix, if you have one.

You will get a response within two working days.

## What counts as a vulnerability here

This project's threat model assumes the model may be adversarial: a customer can
write text into a ticket, and that text reaches the agent that chooses the next
tool call. Anything that lets that text, or the model, get further than intended
is in scope. In particular:

- **Bypassing the confirmation protocol.** Any way to execute a destructive
  action without a valid, matching, unexpired, unconsumed token. Forging a
  token, reusing one against a different target, or reaching a mutation on a
  code path that skips `internal/tools/dispatch.go`.
- **Bypassing profiles or the allowlist.** Reaching an action the active profile
  forbids, including through `whmcs_call_action`.
- **Reaching a blocked action.** `GetClientPassword`, `DecryptPassword`,
  `EncryptPassword`, `CreateSsoToken`, `CreateOAuthCredential`,
  `UpdateOAuthCredential` and `ValidateLogin` must be unreachable in every
  configuration.
- **Leaking a credential or secret** into a tool result, an error message, or
  the audit stream.
- **Leaking personal data** without the explicit opt-in, or without the access
  being audited.
- **Escaping the untrusted-content envelope**, so customer-authored text is
  returned as if it were trusted server output.
- **A misclassified action:** anything classified `read` in
  `internal/registry/classification.go` that in fact modifies state, or anything
  classified `write` that is in fact irreversible.
- **Unbounded resource use:** a request that evades the response size cap, the
  page-size clamp, or the request timeout.

## Not vulnerabilities

- A destructive action succeeding after a valid confirmation token was presented.
  That is the protocol working.
- The model choosing a bad but permitted action. The server constrains what is
  possible, not what is wise.
- `whmcs_call_action` returning more fields than a curated tool. Its output is
  denylist-filtered by design, which is documented in the tool description and
  the README.
- Findings that require an attacker to already hold the WHMCS API credential or
  the deployment's environment.

## Operational guidance

If you believe a deployment has been misused rather than the code being flawed:

1. Revoke the WHMCS API credential in **System Settings > API Credentials**.
   That stops the server immediately; it holds no other access.
2. Pull the audit stream. Every tool call has an operation ID, and confirmation
   issuance and consumption share it, so what was executed and what authorised
   it are both recoverable.
3. Then report, so the gap that allowed it gets closed.
