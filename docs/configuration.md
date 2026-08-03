# Configuration

Every setting is an environment variable prefixed `WHMCS_MCP_`, overridable by a
flag. The server validates its configuration at startup and exits non-zero if
anything is missing or inconsistent, rather than starting and failing every
call.

Every setting is an environment variable prefixed `WHMCS_MCP_`, overridable by a
flag. The server validates its configuration at startup and exits non-zero if
anything is missing or inconsistent, rather than starting and failing every
call.

| Variable |Default | Meaning |
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
choose, and how to scope the WHMCS credential: **[docs/profiles.md](profiles.md)**.

## Keeping the credential out of client config

An MCP client stores its `env` block in plain text: Claude Code writes it into
`~/.claude.json`. That puts a WHMCS API secret at rest in a second place, easy
to forget when rotating and impossible to commit safely.

`-env-file` avoids that. The client config carries a path; the secret stays in
one file.

```sh
install -d -m 0700 ~/.config/toc-whmcs-mcp
install -m 0600 /dev/null ~/.config/toc-whmcs-mcp/env
$EDITOR ~/.config/toc-whmcs-mcp/env
```

```sh
WHMCS_MCP_WHMCS_URL=https://billing.example.com
WHMCS_MCP_API_IDENTIFIER=...
WHMCS_MCP_API_SECRET=...
WHMCS_MCP_PROFILE=readonly
```

Then register the server with no credentials in the client config at all:

```sh
claude mcp add whmcs -- toc-whmcs-mcp -env-file "$HOME/.config/toc-whmcs-mcp/env"
```

Or set `WHMCS_MCP_ENV_FILE` instead of passing the flag.

Details worth knowing:

- **The file must not be readable by other users.** Anything with group or
  other permission bits is refused with the `chmod` command to fix it. A
  warning would be printed to a stderr nobody reads.
- **Values already in the environment win.** The file supplies defaults, so a
  systemd unit or container that sets something explicitly is never silently
  overridden by a stale file. Flags still beat both.
- **There is no automatic `.env` discovery.** The MCP client chooses the working
  directory, so auto-loading would let whatever directory it started in supply
  credentials to a server that talks to a billing system. The path is always
  explicit.
- `export KEY=value`, `#` comments, blank lines and quoted values are all
  tolerated. A line that is not an assignment is an error naming its line
  number, rather than being skipped.

## Choosing a profile

See [profiles.md](profiles.md) for the full permission matrix, how to pick one,
and how to scope the WHMCS credential so the two controls reinforce each other.

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
Kubernetes and systemd is in **[examples/](../examples/)**. The reasoning behind
each choice is in **[docs/deployment.md](deployment.md)**.

Always reference the container image by digest, never a tag, and verify its
cosign signature before trusting it.
