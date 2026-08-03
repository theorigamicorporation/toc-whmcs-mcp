# The action registry

`internal/registry/actions_gen.go` describes all 162 WHMCS actions: name,
category, summary, and every request and response parameter with its type,
requiredness and deprecation status. It is generated from the vendor's published
API reference and committed.

The runtime never contacts `developers.whmcs.com`. Scraping happens at
development time only, so the build has no dependency on a third-party site and
every schema change arrives as a reviewable diff.

## Two files, two different things

| File | Generated? | What it answers |
| --- | --- | --- |
| `actions_gen.go` | **yes**, by `cmd/docgen` | What parameters does this action take? |
| `classification.go` | **no**, by hand | How dangerous is this action? |

The split exists because the vendor documentation answers the first question and
not the second. Nothing in the WHMCS docs says whether `ModuleTerminate`
destroys data or `GetClients` merely reads. That judgement is ours, and it drives
the MCP annotations, which profile permits an action, and whether the
confirmation protocol applies.

Never hand-edit `actions_gen.go`.

## Regenerating

```sh
just gen         # scrape, parse, rewrite actions_gen.go
just gen-check   # fail if the committed file is stale
```

`docgen` fetches the API index, resolves every action slug and category, fetches
each action page with bounded concurrency and a polite delay, parses the request
and response tables, and emits deterministic gofmt-clean Go.

Deterministic matters: CI compares a fresh generation against the committed
file, so ordering that depended on map iteration or scrape completion would
produce spurious diffs. Actions are sorted by category then name; parameters
keep their documented order.

A scheduled workflow runs `gen-check` weekly and opens an issue on drift. It is
not part of CI because it needs a third-party site, and a vendor outage should
not block a pull request.

Review the diff before committing. A parameter that changed from optional to
required, or a new deprecated flag, changes what calls this server will accept.

## When generation fails

```
docgen: 1 action(s) have no safety classification: SomeNewAction
Add each to the classification table in internal/registry/classification.go.
Classify conservatively: anything that moves money, changes provisioning,
alters global configuration, or mails a customer is destructive
```

This is deliberate, and it is the most important safety property of the whole
generation step. A WHMCS upgrade cannot quietly introduce an unclassified
destructive action: a human has to look at it and decide what it does.

At runtime an unclassified action falls back to `ClassWrite`, never
`ClassRead`, so even the unreachable path is safe. Generation failing means that
fallback should never be exercised in a built binary.

## Classifying a new action

Add it to the table in `classification.go`, grouped under its category, with a
trailing comment when the reason is not obvious from the name.

Pick the class by asking **what cannot be undone by the operator who triggered
it**, not by which SQL verb runs:

| Class | Use when | Examples |
| --- | --- | --- |
| `ClassRead` | Performs no modification | `GetInvoices`, `DomainWhois` |
| `ClassWrite` | Modifies one record, an operator can put it back | `UpdateClient`, `AddTicketNote` |
| `ClassDestructive` | Irreversible, moves money, changes provisioning, alters global config, or emails a customer | `DeleteClient`, `AddInvoicePayment`, `DomainRenew`, `ModuleTerminate`, `AddTicketReply` |
| `ClassBlocked` | Returns or mints a credential | `GetClientPassword`, `CreateSsoToken` |

`ClassDestructive` is broader than deletion. `AddTicketReply` is destructive
because a sent email cannot be unsent. `DomainRenew` is destructive because it
spends money at a registrar. `SetConfigurationValue` is destructive because it
changes the system for everyone.

**When in doubt, classify higher.** Over-classifying costs an operator one
confirmation step. Under-classifying costs a customer their server.

Two tests back the table:

- Every action in the registry must have a classification.
- A pinned list of known-dangerous actions must be classified destructive. If
  someone downgrades `ModuleTerminate` or `AddInvoicePayment`, that test fails.

Neither makes a judgement call correct. A classification change is a
CODEOWNERS-reviewed change for that reason, and it belongs in the changelog as a
`feat:` or `fix:`, not a `chore:`.

## Blocking an action outright

`ClassBlocked` means unreachable in every profile and every configuration.
`registry.Resolve` refuses it before policy or validation runs, so it never
reaches the network by any path, including `whmcs_call_action`.

There is deliberately no environment variable that enables one. Unblocking
requires a code change and a review. The seven currently blocked actions all
return or issue credentials, and an agent holding a decrypted customer password
or a minted SSO token has escaped every other control in this server.

## Parameter validation

`registry.Validate` checks arguments against the generated schema before any
network call:

- Unknown parameters are **rejected**, not ignored, and the error lists the
  accepted names. Silently ignoring a misspelling is how a filtered query
  becomes an unfiltered one.
- Required parameters are enforced locally.
- Types are coerced and checked. JSON decodes every number to a float, so an
  integral float is accepted as an integer and a fractional one is not.
- Identifier parameters must be 1 or greater. Zero and negative values are not
  "no filter" in WHMCS; they are undefined behaviour.
- Strings are capped at 32 KiB.
- Deprecated parameters are refused with an explanation. In WHMCS they are
  overwhelmingly raw cardholder data.

Two fuzz targets cover this path, since the arguments come from the model:

```sh
go test -fuzz=FuzzValidate ./internal/registry/
go test -fuzz=FuzzResolveNeverPanics ./internal/registry/
```

## If the vendor redesigns their documentation site

`docgen` parses HTML, so a site redesign breaks it. Two things limit the damage:

- The generated file is committed, so the build never depends on the scrape.
- Golden-file tests parse captured fixture pages, so a parser regression is
  caught by `go test` with no network access.

If parsing breaks, fix `cmd/docgen/parse.go` and update the fixtures in
`cmd/docgen/testdata/`.
