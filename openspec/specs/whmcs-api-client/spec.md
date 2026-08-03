# whmcs-api-client Specification

## Purpose
Defines how the server talks to the WHMCS Admin API: authentication, bounded
network behaviour, runtime validation of responses, and the generated registry
that describes every available WHMCS action and its parameters.
## Requirements
### Requirement: Authenticated WHMCS transport

The client SHALL call the WHMCS Admin API by POSTing form-encoded parameters to
`<base_url>/includes/api.php` with `identifier`, `secret`, `action` and
`responsetype=json`. Credentials SHALL be supplied by configuration and SHALL
never appear in tool output, error messages, or logs.

#### Scenario: Credentials are attached to every call
- **WHEN** any action is invoked
- **THEN** the request body includes the configured `identifier` and `secret` and
  `responsetype=json`

#### Scenario: Credentials never leak into output
- **WHEN** a call fails and the error is rendered to the caller or written to the
  audit log
- **THEN** the rendered text contains neither the `identifier` nor the `secret`,
  even if WHMCS echoed them back

### Requirement: Bounded network behaviour

Every request SHALL carry a timeout and SHALL honour cancellation of the calling
context. The client SHALL refuse to buffer a response beyond a configured
maximum size.

#### Scenario: A stalled WHMCS server does not hang the tool call
- **WHEN** WHMCS accepts the connection but sends no response within the
  configured request timeout
- **THEN** the call is aborted and a typed timeout error is returned

#### Scenario: Caller cancellation propagates
- **WHEN** the MCP request context is cancelled mid-flight
- **THEN** the in-flight HTTP request is aborted and a typed cancellation error is
  returned

#### Scenario: Oversized responses are rejected, not buffered
- **WHEN** a response body exceeds the configured maximum size
- **THEN** reading stops at the limit and a typed response-too-large error is
  returned naming the limit and suggesting a narrower query

### Requirement: Retries only on idempotent reads

The client SHALL retry a failed request only when the action is classified
read-only in the registry and the failure is transient (connection error, or
HTTP 429/502/503/504). Retries SHALL be bounded and use exponential backoff with
jitter. Write actions SHALL never be retried automatically.

#### Scenario: A transient failure on a read is retried
- **WHEN** `GetClients` fails with HTTP 503 and attempts remain
- **THEN** the client waits a backoff interval and retries

#### Scenario: A write is not retried
- **WHEN** `AddInvoicePayment` fails with HTTP 503
- **THEN** no retry is attempted and the error is returned to the caller

### Requirement: Runtime response validation

Responses SHALL be validated at runtime before use. The client SHALL NOT assume a
2xx status or a JSON content type implies a well-formed WHMCS payload.

#### Scenario: An HTML error page is not treated as data
- **WHEN** WHMCS returns an HTML maintenance or login page with HTTP 200
- **THEN** a typed invalid-response error is returned and no partial data is
  passed to the caller

#### Scenario: A WHMCS application error is typed
- **WHEN** the response body is `{"result":"error","message":"Invalid IP"}`
- **THEN** a typed WHMCS API error carrying that message is returned, distinct
  from a transport error

#### Scenario: A successful response is surfaced as structured data
- **WHEN** the response body has `result: success`
- **THEN** the decoded payload is returned to the caller as structured data

### Requirement: Generated action registry

The repository SHALL contain a generated registry describing every WHMCS action:
its name, category, description, request parameters (name, type, required,
deprecated, description), and a read-only/write/destructive classification. The
registry SHALL be produced by a `docgen` command from the official WHMCS API
reference and checked into version control.

#### Scenario: Registry covers the documented API
- **WHEN** the registry is loaded
- **THEN** it contains an entry for every action listed on the WHMCS API index,
  each assigned to its documented category

#### Scenario: Registry is regenerable and diffable
- **WHEN** `docgen` is re-run against unchanged upstream documentation
- **THEN** the generated file is byte-identical to the committed one

#### Scenario: Unknown actions are rejected
- **WHEN** a caller requests an action name absent from the registry
- **THEN** the call is rejected before any network request is made

### Requirement: Parameter validation against the registry

Parameters SHALL be validated against the registry before dispatch: unknown
parameters rejected, required parameters enforced, declared types coerced and
checked, and string lengths bounded.

#### Scenario: A missing required parameter is caught locally
- **WHEN** `AddClient` is called without `email`
- **THEN** a validation error naming the missing parameter is returned and no
  request reaches WHMCS

#### Scenario: A misspelled parameter is caught rather than silently ignored
- **WHEN** a call passes `first_name` where the registry declares `firstname`
- **THEN** a validation error naming the unknown parameter is returned, listing
  the accepted parameter names

#### Scenario: Identifier types are constrained
- **WHEN** a parameter declared `int` receives `-1`, `0` or `1.5` for an entity ID
- **THEN** a validation error is returned

#### Scenario: Deprecated parameters are refused
- **WHEN** a call passes a parameter the registry marks deprecated, such as
  `cardnum` on `AddClient`
- **THEN** the call is rejected with an error explaining the parameter is
  deprecated and must not be sent through this server

