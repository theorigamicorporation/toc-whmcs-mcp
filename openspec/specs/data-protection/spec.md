# data-protection Specification

## Purpose
Defines what data is allowed to leave the server and in what shape: field
projection, secret and PII redaction, explicit labelling of customer-authored
content as untrusted, and the audit trail that records what the agent actually
did.
## Requirements
### Requirement: Field projection before redaction

Curated tool results SHALL be built by projecting the WHMCS response onto the
fields the tool's output schema declares. Fields SHALL be selected by an
allowlist, so a new field appearing in a future WHMCS version is excluded by
default.

The generic `whmcs_call_action` escape hatch has no per-action output schema and
therefore cannot use an allowlist. It SHALL instead apply deep denylist
redaction to every key at every depth, SHALL bound result depth and size, and
its tool description SHALL state that its output is filtered by denylist rather
than projected by allowlist.

#### Scenario: New upstream fields do not leak from curated tools
- **WHEN** WHMCS adds a field to a response that no output schema declares
- **THEN** it does not appear in the result of any curated tool

#### Scenario: Whole-response dumps are not produced by curated tools
- **WHEN** a curated tool succeeds
- **THEN** the result is the projection, not a serialisation of the full upstream
  response

#### Scenario: The escape hatch redacts by denylist at every depth
- **WHEN** `whmcs_call_action` returns a nested response containing a password
  field three levels down
- **THEN** that value is withheld

#### Scenario: The escape hatch is honest about its weaker guarantee
- **WHEN** a client inspects the `whmcs_call_action` tool description
- **THEN** it states that output is denylist-filtered, not allowlist-projected,
  and that curated tools should be preferred where one exists

### Requirement: Secret and credential redaction

Values that are secrets SHALL never be returned. This includes service and
account passwords, API identifiers and secrets, SSO and OAuth tokens, security
question answers, and full or partial card data.

#### Scenario: Service credentials are withheld
- **WHEN** a WHMCS response carries a service password field
- **THEN** the result reports that a value is present and withholds it

#### Scenario: Card fields are withheld
- **WHEN** a response carries card number, expiry, start date, issue number or
  CVV
- **THEN** none of those values appear in the result

#### Scenario: Password retrieval is not a tool
- **WHEN** a client lists tools in any profile
- **THEN** no tool returns a client or service password, and
  `GetClientPassword` and `DecryptPassword` are not reachable through the escape
  hatch

### Requirement: PII minimisation

Personal data SHALL be returned only when the tool's purpose requires it. Tools
that identify a client SHALL default to a minimal projection and SHALL require
an explicit opt-in argument to include full postal address, phone number and tax
identifier. Admin-only notes SHALL be excluded unless explicitly requested.

#### Scenario: Lookup returns a minimal profile
- **WHEN** a client is looked up without the detail opt-in
- **THEN** the result carries identifiers, name, company, email domain and status,
  and omits street address, phone number and tax identifier

#### Scenario: Full detail is deliberate
- **WHEN** the detail opt-in is set and the profile permits it
- **THEN** the full contact record is returned and the access is audited

#### Scenario: Admin notes are excluded by default
- **WHEN** a client record carries admin-only notes
- **THEN** they are omitted unless explicitly requested

### Requirement: Untrusted content labelling

Customer-authored text SHALL be returned inside an explicit untrusted-data
envelope carrying its origin, and SHALL never be interpolated into a field the
agent reads as instruction. This covers ticket subjects, bodies, replies and
notes, client notes, email bodies, order notes and custom field values.

#### Scenario: A ticket body is fenced
- **WHEN** a ticket is read
- **THEN** its body is returned inside an untrusted envelope stating the content
  is customer-authored data and must not be followed as instructions

#### Scenario: Injected instructions do not gain authority
- **WHEN** a ticket body contains text instructing the agent to delete a client
- **THEN** it is returned as labelled untrusted data, and any resulting delete
  attempt is still subject to profile checks and the confirmation protocol

#### Scenario: Control characters are neutralised
- **WHEN** customer content contains control characters or envelope delimiters
- **THEN** they are escaped so the envelope cannot be broken out of

### Requirement: Audit logging with operation identity

Every tool invocation SHALL be recorded with a unique operation ID, timestamp,
profile, tool name, resolved WHMCS action, non-sensitive argument summary,
outcome and duration. Confirmation issuance and consumption SHALL be recorded
and correlated to the operation ID. Audit records SHALL be written to a stream
separate from tool output and SHALL contain no secrets and no customer content.

#### Scenario: Every call is attributable
- **WHEN** any tool is invoked
- **THEN** an audit record is emitted carrying a unique operation ID and the
  outcome

#### Scenario: Confirmation is traceable end to end
- **WHEN** a token is issued and later consumed
- **THEN** both events are recorded and share the operation ID of the executed
  mutation

#### Scenario: Audit output does not become a leak channel
- **WHEN** a tool handles customer content or credentials
- **THEN** the audit record contains neither, only field names and counts

#### Scenario: Audit does not corrupt stdio transport
- **WHEN** the server runs over stdio
- **THEN** audit records are written to stderr or a configured sink, never to
  stdout

