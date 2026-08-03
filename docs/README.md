# Documentation

| Guide | What it covers |
| --- | --- |
| [Security model](security-model.md) | What is enforced, where, and what an attacker would have to defeat |
| [Profiles and access control](profiles.md) | Choosing a profile, the permission matrix, scoping the WHMCS credential |
| [Tool reference](tools.md) | Every advertised tool, its arguments, and worked call sequences |
| [Deployment](deployment.md) | MCP clients, containers, Kubernetes, systemd, the HTTP transport |
| [Troubleshooting](troubleshooting.md) | Error codes, what each one means, and what to do about it |
| [Action registry](registry.md) | Regenerating from vendor docs, and how to classify a new action |

Installation, configuration reference and the profile summary are in the
[main README](../README.md). Ready-to-use configuration lives in
[examples/](../examples/).

Before changing anything under `internal/`, read [CLAUDE.md](../CLAUDE.md) and
[CONTRIBUTING.md](../CONTRIBUTING.md). The specification of record is
[openspec/specs/](../openspec/specs/); behaviour changes go there first.
