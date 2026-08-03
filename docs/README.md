# Documentation

Start with the [main README](../README.md) for what this is and how to install
it. These pages are the detail.

## Running it

| Guide | What it covers |
| --- | --- |
| [Install](install.md) | Every install path, and verifying what you downloaded |
| [Configuration](configuration.md) | Every setting, and connecting an MCP client |
| [Profiles and access control](profiles.md) | Choosing a profile, the permission matrix, scoping the WHMCS credential |
| [Deployment](deployment.md) | Containers, Kubernetes, systemd, the HTTP transport |
| [Troubleshooting](troubleshooting.md) | Error codes, what each means, and what to do about it |

## Understanding it

| Guide | What it covers |
| --- | --- |
| [Security model](security-model.md) | What is enforced, where, and what an attacker would have to defeat |
| [Tool reference](tools.md) | Every advertised tool, its arguments, and worked call sequences |
| [Rationale](rationale.md) | Why this is not a thin one-tool-per-endpoint wrapper |

## Changing it

| Guide | What it covers |
| --- | --- |
| [Development](development.md) | Building from source, testing, project layout |
| [Action registry](registry.md) | Regenerating from vendor docs, and how to classify a new action |
| [Licensing](licensing.md) | Dependency licences, notice obligations, vendor documentation |

Ready-to-use configuration lives in [examples/](../examples/). The
specification of record is [openspec/specs/](../openspec/specs/); behaviour
changes go there first. Working rules for contributors are in
[CONTRIBUTING.md](../CONTRIBUTING.md) and [CLAUDE.md](../CLAUDE.md).
