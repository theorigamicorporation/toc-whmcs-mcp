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

## What it is

WHMCS is production billing infrastructure: customer personal data, payment
records, support correspondence, and credentials for provisioning servers. This
server lets an LLM agent work with it without handing the agent the keys.

All 162 documented WHMCS actions are reachable. Only 15 to 25 tools are
advertised, depending on the profile, so the tool listing stays small enough for
real MCP clients and for useful tool selection.

Every safety control is enforced **inside this process**. Nothing is delegated
to the model's good behaviour, to the MCP host's confirmation dialog, or to how
narrowly the WHMCS credential happens to be scoped. The design assumes the model
may be actively working against you, because a customer can put text in a
support ticket and that text reaches the agent choosing the next tool call.

## Install

```sh
go install github.com/theorigamicorporation/toc-whmcs-mcp/cmd/toc-whmcs-mcp@latest
```

Prebuilt binaries, the container image, and signature verification:
**[docs/install.md](docs/install.md)**. Setting this up as an agent, unattended:
[the block at the end of that page](docs/install.md#for-an-agent-setting-this-up).

```sh
export WHMCS_MCP_WHMCS_URL=https://billing.example.com
export WHMCS_MCP_API_IDENTIFIER=...      # System Settings > API Credentials
export WHMCS_MCP_API_SECRET=...

toc-whmcs-mcp -healthcheck    # can it reach WHMCS and authenticate
toc-whmcs-mcp -print-tools    # what this configuration would expose
```

It starts in the `readonly` profile and advertises nothing that can change data.

## Documentation

| | |
| --- | --- |
| [Install](docs/install.md) | Every install path, and verifying what you downloaded |
| [Configuration](docs/configuration.md) | Every setting, and connecting an MCP client |
| [Profiles](docs/profiles.md) | Choosing one, the permission matrix, scoping the credential |
| [Security model](docs/security-model.md) | What is enforced, where, and what an attacker must defeat |
| [Tool reference](docs/tools.md) | Every tool, its arguments, worked call sequences |
| [Deployment](docs/deployment.md) | Containers, Kubernetes, systemd, the HTTP transport |
| [Troubleshooting](docs/troubleshooting.md) | Error codes and what to do about each |
| [Action registry](docs/registry.md) | Regenerating it, and classifying a new action |
| [Development](docs/development.md) | Building, testing, project layout |
| [Rationale](docs/rationale.md) | Why not a thin one-tool-per-endpoint wrapper |
| [Licensing](docs/licensing.md) | Dependencies, notices, vendor documentation |

Ready-to-edit configuration for MCP clients, Compose, Kubernetes and systemd is
in **[examples/](examples/)**.

## The short version of the security model

- **Read-only by default.** Tools the profile forbids are never registered, so
  they are neither listed nor callable.
- **Four profiles**: `readonly`, `support`, `billing`, `admin`. A support agent
  cannot post a payment; a billing agent cannot terminate a service.
- **Destructive actions are off even under `admin`** and need explicit
  enablement on top of the profile.
- **Two-step confirmation.** A destructive call with no token writes nothing; it
  returns an impact statement and a token bound to those exact arguments, which
  expires and works once. A model cannot mint one.
- **Seven credential-returning actions are unreachable** in every configuration.
- **Annotations are derived**, never hand-set, so `readOnlyHint: false` is
  trustworthy.
- **Data minimisation.** Curated tools project onto declared fields; personal
  data and admin notes need an audited per-call opt-in; credentials are never
  returned.
- **Customer text is labelled untrusted** and returned in an envelope, paired
  with confirmation so an injected agent still cannot execute a mutation alone.
- **Bounded I/O.** Timeouts, response size caps, clamped pagination, and retries
  only on reads, so a 503 cannot post a payment twice.
- **Every call is audited** with an operation ID correlating confirmation
  issuance and consumption.

The detail, including what an attacker would actually have to defeat and the
known limits, is in **[docs/security-model.md](docs/security-model.md)**.

## Status

Specification of record: [openspec/specs/](openspec/specs/). Behaviour changes
go there first.

Licensed under the [GNU AGPL v3.0](LICENSE). If you run a modified version and
let others interact with it over a network, you must offer them its source.
Contributions welcome; see [CONTRIBUTING.md](CONTRIBUTING.md), and
[SECURITY.md](SECURITY.md) for anything security-related, which must not go in a
public issue.
