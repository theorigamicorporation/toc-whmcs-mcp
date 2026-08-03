# Changelog

## [0.1.2](https://github.com/theorigamicorporation/toc-whmcs-mcp/compare/v0.1.1...v0.1.2) (2026-08-03)


### Features

* **config:** read settings from an env file so credentials stay out of client config ([#19](https://github.com/theorigamicorporation/toc-whmcs-mcp/issues/19)) ([a954c84](https://github.com/theorigamicorporation/toc-whmcs-mcp/commit/a954c844b2db9c9c6d3ead0244ced0bbb870b442))


### Bug Fixes

* **docs:** resolve GOBIN when locating the installed binary ([#17](https://github.com/theorigamicorporation/toc-whmcs-mcp/issues/17)) ([c60ac19](https://github.com/theorigamicorporation/toc-whmcs-mcp/commit/c60ac19164376c56b65edbf38e382a85e663439f))

## [0.1.1](https://github.com/theorigamicorporation/toc-whmcs-mcp/compare/v0.1.0...v0.1.1) (2026-08-03)


### Features

* document installing without a checkout, and report the version when go-installed ([#13](https://github.com/theorigamicorporation/toc-whmcs-mcp/issues/13)) ([3c570c7](https://github.com/theorigamicorporation/toc-whmcs-mcp/commit/3c570c7f61c2f07fcb64dd3ac5c4f22bf445dd5f))


### Bug Fixes

* report why WHMCS rejected a call instead of only its status code. A request from an IP that is not on the WHMCS API allowlist returns HTTP 403 with `{"result":"error","message":"Invalid IP ..."}`, and the client discarded that body, reporting "WHMCS rejected the API credential" and sending operators to check a credential that was never the problem ([#15](https://github.com/theorigamicorporation/toc-whmcs-mcp/issues/15)) ([ec39c8a](https://github.com/theorigamicorporation/toc-whmcs-mcp/commit/ec39c8a))

<!-- Added by hand: the commit above was typed `docs:` because the pull request
     was mostly a documentation split, so release-please could not see the fix
     inside it. CONTRIBUTING.md says to pick the type by what an operator sees,
     not by which files moved. This entry is the correction. -->

## 0.1.0 (2026-08-03)


### Features

* MCP server for the WHMCS Admin API ([ccf85c8](https://github.com/theorigamicorporation/toc-whmcs-mcp/commit/ccf85c86130f623ef6a1c9e49a0b23716c68d9b1))


### Bug Fixes

* **registry:** silence a gosec false positive on the description table ([aaf1751](https://github.com/theorigamicorporation/toc-whmcs-mcp/commit/aaf1751f962e815146ba45017bbcf6328a9611ff))

## Changelog

All notable changes to toc-whmcs-mcp are documented here. Entries are generated
by [release-please](https://github.com/googleapis/release-please) from
[Conventional Commits](https://www.conventionalcommits.org/); see
[CONTRIBUTING.md](CONTRIBUTING.md) for how to write one.
