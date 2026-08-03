## Purpose

Defines how the server is configured, transported, built, tested, containerised
and released, so that deployments are reproducible and failures are visible.

## ADDED Requirements

### Requirement: Configuration surface

Configuration SHALL be accepted from environment variables prefixed
`WHMCS_MCP_`, overridable by command-line flags. It SHALL cover the WHMCS base
URL, API credentials, profile, destructive-action enablement, tool allowlist,
transport and bind address, request timeout, maximum response size, default and
maximum page size, confirmation TTL, and log level.

#### Scenario: Flags override environment
- **WHEN** a setting is provided by both environment variable and flag
- **THEN** the flag value is used

#### Scenario: Secrets are not echoed
- **WHEN** the server logs its effective configuration at startup
- **THEN** credential values are masked

### Requirement: Two transports

The server SHALL support stdio and Streamable HTTP, selected by configuration.
Exactly one transport SHALL be active per process.

#### Scenario: Stdio is the default
- **WHEN** no transport is configured
- **THEN** the server serves MCP over stdio and writes no protocol-foreign output
  to stdout

#### Scenario: HTTP binds and serves
- **WHEN** the HTTP transport is selected with a bind address
- **THEN** the server serves Streamable HTTP on that address

#### Scenario: HTTP is not open by default
- **WHEN** the HTTP transport is selected without an authentication token
  configured
- **THEN** the server refuses to start unless the bind address is loopback

### Requirement: Single source of version truth

The binary, the MCP server identification, the container image label and the
release tag SHALL report the same version string, injected at build time.

#### Scenario: Reported versions agree
- **WHEN** the binary prints its version and an MCP client reads the server
  implementation version
- **THEN** the two strings are identical

### Requirement: Meaningful container health check

The container image SHALL define a health check that verifies the server process
is serving and can reach WHMCS. A check that only proves the runtime can execute
a statement SHALL NOT be used.

#### Scenario: Broken upstream is unhealthy
- **WHEN** the process is running but WHMCS is unreachable
- **THEN** the health check reports unhealthy

#### Scenario: Working server is healthy
- **WHEN** the process is serving and a WHMCS connectivity probe succeeds
- **THEN** the health check reports healthy

### Requirement: Reproducible builds and pinned images

The container base image SHALL be pinned by digest. Released images SHALL be
referenced by immutable tag or digest in documentation, not a moving tag.

#### Scenario: Base image is pinned
- **WHEN** the Dockerfile is inspected
- **THEN** its base image is referenced by digest

#### Scenario: Documentation avoids moving tags
- **WHEN** deployment documentation names an image
- **THEN** it uses a version tag or digest, not `latest`

### Requirement: Task runner

The repository SHALL provide a `justfile` whose default recipe lists available
recipes, grouped by category and colourised, covering setup, code generation,
build, test, lint, format, container image and release.

#### Scenario: Bare invocation is a menu
- **WHEN** `just` is run with no arguments
- **THEN** the grouped recipe list is printed and nothing is built or deployed

### Requirement: Tests run without a live WHMCS

The test suite SHALL run offline against fakes. No test SHALL require WHMCS
credentials or network access, and no test SHALL print real customer data.

#### Scenario: Offline test run passes
- **WHEN** tests run with no network access and no credentials configured
- **THEN** the suite passes

#### Scenario: Protocol behaviour is covered
- **WHEN** the suite runs
- **THEN** it includes in-process MCP tests asserting tool listing, annotation
  values, profile denial, the confirmation protocol, redaction and pagination
  clamping

#### Scenario: CI does more than compile
- **WHEN** CI runs on a pull request
- **THEN** it builds, vets, lints, runs the full test suite with the race
  detector, and verifies the generated registry is up to date
