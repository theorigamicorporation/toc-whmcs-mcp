# systemd

`toc-whmcs-mcp.service` runs the HTTP transport as a hardened system service.

```sh
sudo install -m 0755 bin/toc-whmcs-mcp /usr/local/bin/
sudo install -m 0644 toc-whmcs-mcp.service /etc/systemd/system/
sudo install -d -m 0700 /etc/toc-whmcs-mcp
sudo install -m 0600 ../env/http.env /etc/toc-whmcs-mcp/http.env
sudo systemctl daemon-reload
sudo systemctl enable --now toc-whmcs-mcp
```

Check what it is allowed to do:

```sh
systemd-analyze security toc-whmcs-mcp
```

`IPAddressDeny=any` with an explicit allow for the WHMCS host is the setting
worth taking the time over. It is the control that still holds if the server
itself has a bug: a compromised process cannot reach anything else on the
network. Uncomment and set the address once you know it.

For the stdio transport there is no unit to write. The MCP client starts the
process itself.
