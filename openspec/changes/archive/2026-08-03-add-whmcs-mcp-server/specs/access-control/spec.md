## Purpose

Defines who may do what: the capability profiles that determine which tools
exist at all, the optional per-tool allowlist, and the prepare/confirm nonce
protocol that makes a consequential operation require a deliberate second step.

## ADDED Requirements

### Requirement: Read-only by default

The server SHALL start in the `readonly` profile when no profile is configured.
Elevating to a write-capable profile SHALL require explicit configuration.

#### Scenario: Default start is read-only
- **WHEN** the server starts with no profile configured
- **THEN** the active profile is `readonly` and no write or destructive tool is
  advertised

#### Scenario: The active profile is discoverable
- **WHEN** an operator inspects the server's startup log or its status tool
- **THEN** the active profile and the count of enabled tools are reported

### Requirement: Capability profiles

The server SHALL implement the profiles `readonly`, `support`, `billing` and
`admin`. Each profile SHALL define a fixed set of permitted actions. A tool whose
underlying action is not permitted by the active profile SHALL NOT be advertised
and SHALL be refused if invoked.

#### Scenario: Support cannot touch billing
- **WHEN** the `support` profile is active and an invoice payment is attempted
- **THEN** the call is refused with `forbidden`

#### Scenario: Billing cannot terminate services
- **WHEN** the `billing` profile is active and a service termination is attempted
- **THEN** the call is refused with `forbidden`

#### Scenario: Support can read and draft but not auto-send
- **WHEN** the `support` profile is active
- **THEN** ticket reads and reply drafting are permitted, and any action that
  sends mail to a customer requires confirmation

### Requirement: Destructive actions are opt-in even under admin

Actions classified destructive SHALL be disabled by default in every profile,
including `admin`, and SHALL require explicit enablement through configuration.

#### Scenario: Admin alone is not enough
- **WHEN** the `admin` profile is active and destructive actions have not been
  explicitly enabled
- **THEN** `DeleteClient` is refused with `forbidden`

#### Scenario: Explicit enablement works
- **WHEN** the `admin` profile is active and destructive actions are explicitly
  enabled in configuration
- **THEN** `DeleteClient` becomes available, still subject to confirmation

### Requirement: Per-tool allowlist

Configuration SHALL support an explicit allowlist that further narrows the
profile. When set, only listed tools SHALL be advertised or callable. The
allowlist SHALL only subtract from the profile, never add to it.

#### Scenario: Allowlist narrows the profile
- **WHEN** the `admin` profile is active and the allowlist names three tools
- **THEN** exactly those three tools are advertised

#### Scenario: Allowlist cannot escalate
- **WHEN** the `readonly` profile is active and the allowlist names a write tool
- **THEN** that tool is still not advertised, and startup logs the ignored entry

### Requirement: Prepare and confirm protocol

A consequential operation SHALL NOT execute on its first call. The first call
SHALL return an impact preview and a server-generated confirmation nonce.
Execution SHALL require that nonce to be presented on a second call.

#### Scenario: First call previews and does not execute
- **WHEN** a service termination is invoked without a nonce
- **THEN** no WHMCS write occurs, and the result contains a description of what
  would change plus a `confirmation_token`

#### Scenario: Second call with the token executes
- **WHEN** the same operation is invoked with the returned token, unexpired
- **THEN** the operation executes exactly once

#### Scenario: A natural-language assertion is not a substitute
- **WHEN** the caller supplies a token it invented, or claims in its arguments
  that the user approved
- **THEN** the call is refused with `confirmation_required`

### Requirement: Nonce binding, expiry and single use

A nonce SHALL be bound to the exact action and normalised argument set that
produced it, SHALL expire after a configured TTL, and SHALL be consumed on first
successful use.

#### Scenario: A token cannot be moved to another target
- **WHEN** a token issued for terminating service 42 is presented for service 43
- **THEN** the call is refused with `confirmation_mismatch`

#### Scenario: A token cannot be moved to another action
- **WHEN** a token issued for a client update is presented for a client deletion
- **THEN** the call is refused with `confirmation_mismatch`

#### Scenario: An expired token is refused
- **WHEN** a token is presented after its TTL has elapsed
- **THEN** the call is refused with `confirmation_expired`

#### Scenario: Replay executes once
- **WHEN** the same valid token is presented twice
- **THEN** the operation executes on the first presentation and the second is
  refused with `confirmation_consumed`

### Requirement: Fail-fast configuration

The server SHALL validate its configuration at startup and SHALL exit non-zero
with a clear message if the WHMCS base URL or credentials are missing or
malformed. It SHALL NOT start and advertise tools it cannot execute.

#### Scenario: Missing credentials stop startup
- **WHEN** the server starts without an API identifier or secret
- **THEN** it exits non-zero naming the missing settings, and no MCP session is
  served

#### Scenario: Unknown profile stops startup
- **WHEN** the configured profile is not one of the four defined profiles
- **THEN** the server exits non-zero listing the valid profiles
