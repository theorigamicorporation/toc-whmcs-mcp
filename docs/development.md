# Development

## Building from source

Only needed if you are changing the code; the install paths above do not
require a checkout.

```bash
git clone https://github.com/theorigamicorporation/toc-whmcs-mcp.git
cd toc-whmcs-mcp
just setup                 # install the pinned toolchain and dependencies
just setup-env             # create .env from .env.example, then fill it in
set -a; source .env; set +a

just tools                 # see what this configuration would advertise
just healthcheck           # verify connectivity and the credential
just test                  # full suite: no network, no credentials needed
```

Create the API credential in WHMCS under **System Settings > API Credentials**,
and scope its role to the minimum the profile needs.

## Working on it

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
is the release note. See [CONTRIBUTING.md](../CONTRIBUTING.md).

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

Report vulnerabilities privately: see [SECURITY.md](../SECURITY.md).
The full security model, including what an attacker would have to defeat, is in
**[docs/security-model.md](security-model.md)**.

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

## Where the risk is

Two files carry most of it, and changes to either want a careful review rather
than a rubber stamp: `internal/registry/classification.go` decides what an
action is allowed to do, and `internal/tools/dispatch.go` is the single path
every tool call takes. See [CONTRIBUTING.md](../CONTRIBUTING.md).
