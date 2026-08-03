# Deployment

Ready-to-copy files are in [examples/](../examples/). This explains the choices
behind them.

## Before anything else

1. Create an API credential in WHMCS: **System Settings → API Credentials**.
   One credential per deployment, scoped to the minimum its profile needs. See
   [profiles.md](profiles.md#scope-the-whmcs-credential-too).
2. If WHMCS restricts API access by IP, allowlist the deployment's **egress** IP,
   which for a container is often not the host address.
3. Decide the profile. `readonly` unless there is a reason.

Verify before wiring an agent to it:

```sh
toc-whmcs-mcp -healthcheck   # reaches WHMCS and authenticates
toc-whmcs-mcp -print-tools   # what this configuration would advertise
```

## Choosing a transport

**stdio** is the default and the right choice for a local MCP client: Claude
Code, Claude Desktop, an IDE extension. The client starts the process and owns
its lifetime. Nothing listens on a port.

**Streamable HTTP** is for one shared server used by several clients, or an
agent running somewhere the binary cannot. It is more surface: a listening
socket, a token to manage, and no per-user identity.

Prefer stdio. Reach for HTTP when you actually need a shared endpoint.

## Local MCP client (stdio)

Claude Code:

```sh
claude mcp add whmcs \
  -e WHMCS_MCP_WHMCS_URL=https://billing.example.com \
  -e WHMCS_MCP_API_IDENTIFIER=... \
  -e WHMCS_MCP_API_SECRET=... \
  -e WHMCS_MCP_PROFILE=readonly \
  -- /usr/local/bin/toc-whmcs-mcp
```

Any client taking JSON config: see
[examples/mcp-client/](../examples/mcp-client/), which has one file per profile.

Under stdio, stdout is the protocol channel. The server writes audit and
diagnostics to stderr only. If you wrap the binary in a shell script, make sure
it does not echo anything to stdout.

## Container

Always reference an **immutable digest**, not a tag. A moving tag means the
image you tested is not necessarily the image you deployed.

```sh
docker run --rm -i \
  -e WHMCS_MCP_WHMCS_URL=https://billing.example.com \
  -e WHMCS_MCP_API_IDENTIFIER=... \
  -e WHMCS_MCP_API_SECRET=... \
  -e WHMCS_MCP_PROFILE=readonly \
  ghcr.io/theorigamicorporation/toc-whmcs-mcp@sha256:...
```

`-i` matters: stdio needs stdin held open.

Verify the image signature before you trust it. Releases are signed with
keyless cosign, so there is no key to distribute:

```sh
cosign verify ghcr.io/theorigamicorporation/toc-whmcs-mcp@sha256:... \
  --certificate-identity-regexp='^https://github.com/theorigamicorporation/toc-whmcs-mcp/' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

The image is distroless: no shell, no package manager, no interpreter. There is
nothing in it to run except the server, which is the point for something holding
a billing credential. It runs as `nonroot`.

Its health check calls WHMCS with the configured credential, so it reports
unhealthy when the instance is unreachable or the credential is rejected, not
merely when the process has died.

Compose: [examples/docker-compose.yaml](../examples/docker-compose.yaml).

## Kubernetes

[examples/kubernetes/](../examples/kubernetes/) has a Deployment, Service and
SealedSecret placeholder for the HTTP transport.

Points worth keeping:

- Credentials from a Secret, never from the manifest. The repo convention is
  GitOps: no manual `kubectl` mutation, everything through committed YAML.
- `readOnlyRootFilesystem: true`, `runAsNonRoot: true`,
  `allowPrivilegeEscalation: false`, all capabilities dropped. The binary is
  static and writes nothing to disk.
- Liveness and readiness both run `-healthcheck`, so a pod with a revoked
  credential is taken out of service rather than serving errors.
- **One replica.** Confirmation tokens live in memory, so a second replica would
  reject tokens issued by the first. Running more needs a shared store, which
  this version does not have.
- A NetworkPolicy restricting egress to the WHMCS host, and ingress to the
  namespaces that should reach it.

## systemd

[examples/systemd/](../examples/systemd/) has a unit for the HTTP transport.
`EnvironmentFile` with mode `0600` owned by the service user, plus the usual
hardening: `NoNewPrivileges`, `ProtectSystem=strict`, `PrivateTmp`,
`ProtectHome`, `DynamicUser`.

## The HTTP transport

```sh
WHMCS_MCP_TRANSPORT=http
WHMCS_MCP_ADDR=0.0.0.0:8080
WHMCS_MCP_AUTH_TOKEN=<long random string>
```

The server **refuses to start** if the address is not loopback and no token is
set. An unauthenticated HTTP transport in front of a billing system is a remote
control plane for it.

Understand what the token is: a coarse gate answering "may you talk to this
server". It is not an identity layer, and the audit stream cannot attribute a
call to a person through it. If you need per-user attribution, front it with
something that authenticates users, and give each team its own instance with its
own profile and its own WHMCS credential.

Terminate TLS in front of it. The server speaks plain HTTP.

## Running more than one profile

The common shape is several instances rather than one permissive one:

| Instance | Profile | Destructive | WHMCS credential |
| --- | --- | --- | --- |
| `whmcs-mcp-readonly` | `readonly` | no | read-scoped role |
| `whmcs-mcp-support` | `support` | no | ticket/client role |
| `whmcs-mcp-billing` | `billing` | yes | billing role |

Each agent connects to the instance matching its job. A compromise of the
support agent cannot reach billing, because that path does not exist in its
process, not merely because a check would refuse it.

## Operational checklist

- [ ] Credential scoped in WHMCS to the profile's needs, not shared with another
      environment
- [ ] Egress IP allowlisted in WHMCS if API IP restriction is on
- [ ] Profile set deliberately; `WHMCS_MCP_ALLOW_DESTRUCTIVE` only where needed
- [ ] Image referenced by digest, signature verified
- [ ] Audit stream shipped somewhere durable, with an alert on
      `confirmation.rejected`
- [ ] `-healthcheck` wired to liveness and readiness
- [ ] Single replica, or a plan for shared confirmation state
- [ ] HTTP transport authenticated and TLS-terminated, or stdio only
- [ ] Someone knows that revoking the WHMCS credential is the kill switch

## Upgrading

Releases follow semver, driven by conventional commits. Read `CHANGELOG.md`
before upgrading: a `feat!:` entry means something previously permitted is now
forbidden, or a tool's arguments changed.

The registry-drift workflow opens an issue when WHMCS changes its API. That
usually means regenerating and reviewing a classification, not an urgent
upgrade; see [registry.md](registry.md).
