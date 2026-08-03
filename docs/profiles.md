# Profiles and access control

A profile decides which WHMCS actions this process may perform at all. It
resolves once, at startup: tools the profile forbids are never registered, so
they are neither listed to the model nor callable.

## Choosing one

| If the agent is doing this | Use | Also set |
| --- | --- | --- |
| Answering questions, drafting summaries, investigating | `readonly` | nothing |
| Support triage: reading and replying to tickets, fixing contact details | `support` | `ALLOW_DESTRUCTIVE=true` only if it must send replies |
| Billing work: invoices, quotes, recording payments taken elsewhere | `billing` | `ALLOW_DESTRUCTIVE=true` for payments |
| Provisioning: suspending, unsuspending, terminating services | `admin` | `ALLOW_DESTRUCTIVE=true` |
| You are not sure | `readonly` | nothing |

`readonly` is the default. Starting the server with no profile configured is not
a way to get write access.

## The matrix

Reads are permitted in every category for every profile. A support agent who
cannot see an invoice cannot answer a billing question, so the separation that
matters is over who may **change** what.

| Category | readonly | support | billing | admin |
| --- | :---: | :---: | :---: | :---: |
| Read anything | yes | yes | yes | yes |
| Tickets, announcements | no | **yes** | no | yes |
| Client records, contacts | no | **yes** | **yes** | yes |
| Users, permissions | no | **yes** | no | yes |
| Invoices, quotes, transactions | no | no | **yes** | yes |
| Orders | no | no | **yes** | yes |
| Products, addons | no | no | **yes** | yes |
| Services, provisioning | no | no | no | **yes** |
| Domains, registrars | no | no | no | **yes** |
| System configuration, modules | no | no | no | **yes** |

Advertised tool counts:

| | readonly | support | billing | admin |
| --- | :---: | :---: | :---: | :---: |
| Destructive disabled | 15 | 19 | 16 | 20 |
| Destructive enabled | 15 | 20 | 18 | 25 |

Check any configuration without attaching a client:

```sh
just tools -profile support -allow-destructive
```

## Destructive actions

Off by default in **every** profile, including `admin`. Enable with
`WHMCS_MCP_ALLOW_DESTRUCTIVE=true`.

Enabling does not make them one-step. Each call still returns a preview and a
confirmation token first; see
[the security model](security-model.md#two-step-confirmation).

An action is destructive if it is irreversible by the operator who triggered it.
That includes things that are not deletions:

| Kind | Examples |
| --- | --- |
| Deletion | `DeleteClient`, `DeleteOrder`, `DeleteTicket`, `CloseClient` |
| Moves money | `AddInvoicePayment`, `CapturePayment`, `AddCredit`, `ApplyCredit` |
| Spends money externally | `DomainRegister`, `DomainRenew`, `DomainTransfer` |
| Changes provisioning | `ModuleCreate`, `ModuleSuspend`, `ModuleTerminate`, `ModuleChangePw` |
| Emails a customer | `AddTicketReply`, `SendEmail`, `SendQuote`, `CreateClientInvite` |
| Global configuration | `SetConfigurationValue`, `UpdateModuleConfiguration`, `ActivateModule` |
| Access changes | `ResetPassword`, `DeleteUserClient`, `UpdateUserPermissions` |

## Narrowing further with an allowlist

```sh
WHMCS_MCP_TOOL_ALLOWLIST=whmcs_client_get,whmcs_invoice_list,whmcs_ticket_list
```

The allowlist can only **subtract** from the profile. Naming a write tool under
`readonly` does not enable it; the entry is ignored and logged at startup:

```json
{"level":"WARN","msg":"allowlist entries were ignored because the active profile
does not grant them; the allowlist narrows a profile, it cannot widen one",
"ignored":["whmcs_client_update"],"profile":"readonly"}
```

Use it when a specific agent needs a narrow slice of a profile: a status-page
bot that should only read service state, say.

## Scope the WHMCS credential too

This server narrows what a credential can do. It never widens it. The two should
reinforce each other, so that a bug in either one is not sufficient on its own.

Provision **one credential per profile** in WHMCS under
**System Settings → API Credentials**, and give each API role only the
permissions its profile needs:

| Server profile | WHMCS API role should permit |
| --- | --- |
| `readonly` | the `Get*` actions you actually use, nothing else |
| `support` | ticket and client actions, no billing, no provisioning |
| `billing` | invoice, quote, transaction and order actions, no provisioning |
| `admin` | the full set the deployment genuinely needs |

Do not share one credential between environments, and revoke it when the
deployment goes away. Revoking the credential is also the fastest way to stop a
misbehaving agent: it removes all access immediately, and this server holds no
other.

## Verifying a deployment

```sh
just tools                    # what this configuration would advertise
just healthcheck              # can it reach WHMCS and authenticate
```

At runtime, the `whmcs_status` tool reports the same posture to the agent
itself: active profile, whether destructive actions are enabled, how many
actions are permitted, and the page-size limits.

The startup log records it once, so a log always answers "what was this process
allowed to do":

```json
{"msg":"server.start","profile":"support","allow_destructive":false,
 "tools_advertised":19,"actions_permitted":97,"actions_total":162,
 "api_secret":"(set, 32 chars)"}
```
