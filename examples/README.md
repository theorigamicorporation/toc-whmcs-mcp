# Examples

Configuration you can copy and edit. Every file is commented with why it is
shaped the way it is, not only what it sets.

| Path | What it is |
| --- | --- |
| [`mcp-client/`](mcp-client/) | MCP client configuration, one file per profile |
| [`env/`](env/) | Environment files per profile, for containers and systemd |
| [`docker-compose.yaml`](docker-compose.yaml) | Single instance over HTTP, behind a token |
| [`docker-compose.multi-profile.yaml`](docker-compose.multi-profile.yaml) | Separate readonly, support and billing instances |
| [`kubernetes/`](kubernetes/) | Deployment, Service, Secret and NetworkPolicy |
| [`systemd/`](systemd/) | Hardened unit for the HTTP transport |

Two rules run through all of them:

**Reference images by digest, never by tag.** A moving tag means the image you
tested is not necessarily the image you deployed. Every example uses a
`@sha256:...` placeholder; fill it from the release notes and verify the
signature (see [deployment.md](../docs/deployment.md#container)).

**One credential per profile.** These examples assume separate WHMCS API
credentials, each scoped in WHMCS to what its profile needs. Sharing one
permissive credential across instances defeats the point of running them
separately.

Start with [`env/readonly.env`](env/readonly.env). It is the safest thing that
does something useful.
