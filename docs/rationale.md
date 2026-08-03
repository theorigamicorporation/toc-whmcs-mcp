# Why this exists rather than a thin wrapper

WHMCS is production billing infrastructure. It holds customer personal data,
payment records, support correspondence and credentials for provisioning
servers. A thin one-tool-per-endpoint wrapper around it has three problems that
are not stylistic:

1. The agent can be prompt-injected by the very data the server returns.
   Ticket bodies and client notes are written by customers, and they reach the
   same model that chooses the next tool call.
2. A single API credential authorises everything the WHMCS role permits, and
   the endpoint gives no hint whether an action reads or destroys.
3. Full responses put every customer's personal data into model context and
   provider logs, whether or not the task needed it.

Every control below is enforced inside this process. Nothing is delegated to the
model's good behaviour, to the MCP host's confirmation dialog, or to how
narrowly the WHMCS credential happens to be scoped.

The controls that follow from this are in [security-model.md](security-model.md).
