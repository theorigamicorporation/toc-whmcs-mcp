# mcp-tool-surface Specification

## Purpose
Defines the tools the server advertises over MCP: how many there are, how they
are annotated and schema'd, how results are shaped, and how the generic escape
hatch keeps every WHMCS action reachable without flooding the model's context.
## Requirements
### Requirement: Bounded advertised tool surface

The server SHALL advertise a bounded set of tools rather than one tool per WHMCS
action. The advertised set SHALL consist of curated tools for common workflows
plus a generic action escape hatch, and SHALL NOT exceed 30 tools in any profile.

#### Scenario: Tool listing stays small
- **WHEN** an MCP client lists tools in the `admin` profile
- **THEN** at most 30 tools are returned

#### Scenario: Coverage is not lost
- **WHEN** an operator needs a WHMCS action with no curated tool, such as
  `GetAutomationLog`
- **THEN** it remains callable through the escape hatch

### Requirement: Accurate tool annotations

Every advertised tool SHALL declare `readOnlyHint`, `destructiveHint`,
`idempotentHint` and `openWorldHint`, and those values SHALL match the tool's
actual behaviour. Annotations SHALL be derived from the registry classification
rather than set by hand per tool.

#### Scenario: A read tool is marked read-only
- **WHEN** a client inspects `whmcs_client_get`
- **THEN** `readOnlyHint` is true and `destructiveHint` is false

#### Scenario: A destructive tool is marked destructive
- **WHEN** a client inspects the tool that terminates a service
- **THEN** `readOnlyHint` is false and `destructiveHint` is true

#### Scenario: The escape hatch is annotated by worst case
- **WHEN** a client inspects `whmcs_call_action`
- **THEN** it is annotated non-read-only and destructive, because the action is
  chosen at call time

### Requirement: Structured results with declared output schemas

Tools SHALL declare an `outputSchema` and return `structuredContent` conforming
to it. Tools SHALL NOT return the raw WHMCS response serialised into a text
block.

#### Scenario: A result is machine-readable
- **WHEN** `whmcs_invoice_get` succeeds
- **THEN** the result carries `structuredContent` matching the tool's declared
  `outputSchema`

#### Scenario: Undeclared fields are dropped
- **WHEN** WHMCS returns fields the tool's output schema does not declare
- **THEN** those fields are not included in the result

### Requirement: Registry-backed escape hatch

The server SHALL expose exactly three generic tools: `whmcs_list_actions`,
`whmcs_describe_action` and `whmcs_call_action`. Listing SHALL return names and
one-line summaries only. Describing SHALL return the full parameter schema for a
single action. Calling SHALL validate against the registry and SHALL be subject
to the same profile, confirmation, redaction and bounding rules as curated tools.

#### Scenario: Listing is cheap
- **WHEN** `whmcs_list_actions` is called with a category filter
- **THEN** it returns action names and one-line summaries for that category, not
  full parameter schemas

#### Scenario: Describing is precise
- **WHEN** `whmcs_describe_action("AddOrder")` is called
- **THEN** the full parameter list with types, requiredness and descriptions is
  returned

#### Scenario: The escape hatch is not a policy bypass
- **WHEN** `whmcs_call_action("DeleteClient", ...)` is called in the `readonly`
  profile
- **THEN** it is denied by policy exactly as a curated delete tool would be

#### Scenario: The escape hatch enforces confirmation
- **WHEN** `whmcs_call_action` targets an action classified destructive and no
  valid nonce is supplied
- **THEN** the call returns an impact preview and a nonce instead of executing

### Requirement: Bounded pagination

Every tool returning a collection SHALL accept `limit` and `offset`, SHALL apply
a default limit, and SHALL clamp `limit` to a configured maximum. Results SHALL
report the applied limit and whether more records exist.

#### Scenario: An unbounded request is clamped
- **WHEN** a tool is called with `limit: 100000`
- **THEN** the effective limit is the configured maximum and the result states
  the clamp was applied

#### Scenario: Omitting the limit does not fetch everything
- **WHEN** a collection tool is called with no `limit`
- **THEN** the configured default limit is applied

#### Scenario: Truncation is visible
- **WHEN** more records exist beyond the returned page
- **THEN** the result reports the total count and the next offset

### Requirement: Consistent MCP error semantics

Tool failures SHALL be returned as MCP tool results with `isError` set and a
structured payload carrying a stable machine-readable code, a sanitised message
and a retryable flag. Handlers SHALL NOT let raw errors escape as protocol-level
failures, and SHALL NOT return an error document as a successful result.

#### Scenario: A validation failure is a tool error
- **WHEN** a required parameter is missing
- **THEN** the result has `isError` true and a code of `invalid_params`

#### Scenario: A denial is distinguishable from a failure
- **WHEN** the active profile forbids the tool
- **THEN** the result has `isError` true, a code of `forbidden`, and
  `retryable` false

#### Scenario: Transient upstream failure is marked retryable
- **WHEN** WHMCS is unreachable
- **THEN** the result has `isError` true, a code of `upstream_unavailable`, and
  `retryable` true

### Requirement: No sensitive MCP resources or high-impact prompts

The server SHALL NOT expose MCP resources containing live customer, staff,
server or billing data, and SHALL NOT ship prompt templates that orchestrate
bulk or consequential operations.

#### Scenario: No live data behind resources
- **WHEN** an MCP client lists resources
- **THEN** no resource returns client, invoice, ticket, admin or server records

#### Scenario: No bulk-action prompts
- **WHEN** an MCP client lists prompts
- **THEN** no prompt instructs bulk emailing, bulk renewal or automated fraud
  decisions

