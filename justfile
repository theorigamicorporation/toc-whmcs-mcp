# toc-whmcs-mcp — task runner
# `just` with no arguments shows the grouped recipe list and does nothing else.

set shell := ["bash", "-uc"]

binary  := "toc-whmcs-mcp"
pkg     := "./cmd/toc-whmcs-mcp"
out     := "bin"
image   := "ghcr.io/theorigamicorporation/toc-whmcs-mcp"

version := `git describe --tags --always --dirty 2>/dev/null || echo dev`
commit  := `git rev-parse --short HEAD 2>/dev/null || echo unknown`
ldflags := "-s -w" \
    + " -X github.com/theorigamicorporation/toc-whmcs-mcp/internal/version.Version=" + version \
    + " -X github.com/theorigamicorporation/toc-whmcs-mcp/internal/version.Commit=" + commit

[private]
default:
    @printf '\033[1;35m toc-whmcs-mcp\033[0m  \033[2mMCP server for the WHMCS Admin API\033[0m\n'
    @printf ' \033[2mversion={{ version }}  commit={{ commit }}\033[0m\n\n'
    @just --list --unsorted --list-heading '' --list-prefix '  '
    @printf '\n \033[2mThe server starts read-only. See README.md for the security model.\033[0m\n'

# ── setup ────────────────────────────────────────────────────────────────────

# install the Go toolchain pinned in .tool-versions and download dependencies
[group('setup')]
setup:
    @command -v asdf >/dev/null && asdf install || true
    go mod download
    @printf '\033[32m✓ dependencies ready\033[0m  \033[2m(go %s)\033[0m\n' "$(go env GOVERSION)"

# install the dev tools CI uses, at the versions CI uses
[group('setup')]
setup-tools:
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
    go install github.com/securego/gosec/v2/cmd/gosec@v2.28.0
    go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
    go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
    @printf '\033[32m✓ dev tools installed\033[0m  \033[2mgolangci-lint, gosec, actionlint, govulncheck\033[0m\n'

# install the commit-msg hook that enforces conventional commits
[group('setup')]
setup-hooks:
    @command -v pre-commit >/dev/null || (printf '\033[31m✗ pre-commit not installed: https://pre-commit.com/#install\033[0m\n'; exit 1)
    pre-commit install --hook-type commit-msg
    @printf '\033[32m✓ commit-msg hook installed\033[0m  \033[2mconventional commits drive the version and changelog\033[0m\n'

# write a .env you can edit, then `set -a; source .env; set +a`
[group('setup')]
setup-env:
    @test -f .env && printf '\033[33m! .env already exists, leaving it alone\033[0m\n' || (cp .env.example .env && printf '\033[32m✓ .env created from .env.example\033[0m\n')

# ── generate ─────────────────────────────────────────────────────────────────

# regenerate the action registry from the published WHMCS API reference
[group('generate')]
gen:
    go run ./cmd/docgen
    gofmt -w internal/registry/actions_gen.go
    @printf '\033[32m✓ registry regenerated\033[0m  \033[2mreview the diff before committing\033[0m\n'

# fail if the committed registry is stale (needs network; CI runs this on a schedule)
[group('generate')]
gen-check:
    go run ./cmd/docgen -check

# regenerate the README status badges under docs/badges/
[group('generate')]
badges:
    ./scripts/gen-badges.sh

# ── build ────────────────────────────────────────────────────────────────────

# build the server binary into bin/
[group('build')]
build:
    @mkdir -p {{ out }}
    CGO_ENABLED=0 go build -trimpath -ldflags "{{ ldflags }}" -o {{ out }}/{{ binary }} {{ pkg }}
    @printf '\033[32m✓ built\033[0m {{ out }}/{{ binary }}  \033[2m%s\033[0m\n' "$({{ out }}/{{ binary }} --version)"

# build for linux/amd64 and linux/arm64
[group('build')]
build-all:
    @mkdir -p {{ out }}
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "{{ ldflags }}" -o {{ out }}/{{ binary }}-linux-amd64 {{ pkg }}
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "{{ ldflags }}" -o {{ out }}/{{ binary }}-linux-arm64 {{ pkg }}
    @printf '\033[32m✓ cross-built\033[0m  \033[2m%s\033[0m\n' "$(ls {{ out }} | tr '\n' ' ')"

# remove build output
[group('build')]
clean:
    rm -rf {{ out }} coverage.out coverage.html
    @printf '\033[32m✓ cleaned\033[0m\n'

# ── test ─────────────────────────────────────────────────────────────────────

# run the full suite (no network, no credentials needed)
[group('test')]
test:
    go test ./...

# run the suite with the race detector and shuffled order, as CI does
[group('test')]
test-race:
    go test -race -shuffle=on ./...

# run the fuzz targets on the parsers that handle attacker-influenced input
[group('test')]
fuzz seconds="30":
    go test -run=XXX -fuzz='FuzzWrap$' -fuzztime={{ seconds }}s ./internal/untrusted/
    go test -run=XXX -fuzz='FuzzWrapIsIdempotent' -fuzztime={{ seconds }}s ./internal/untrusted/
    go test -run=XXX -fuzz='FuzzValidate' -fuzztime={{ seconds }}s ./internal/registry/
    go test -run=XXX -fuzz='FuzzResolveNeverPanics' -fuzztime={{ seconds }}s ./internal/registry/
    @printf '\033[32m✓ fuzz targets clean\033[0m\n'

# enforce the coverage floors CI enforces
[group('test')]
coverage-gate threshold="65" safety="80":
    #!/usr/bin/env bash
    set -euo pipefail
    go test -coverpkg=./... -coverprofile=coverage.out -covermode=atomic ./... >/dev/null
    total=$(go tool cover -func=coverage.out | grep '^total:' | awk '{print $3}' | tr -d '%')
    printf 'total %s%% (floor {{ threshold }}%%)\n' "$total"
    fail=0
    awk "BEGIN {exit !($total < {{ threshold }})}" && { printf '\033[31m✗ total below floor\033[0m\n'; fail=1; }
    for pkg in policy confirm redact shape untrusted whmcs; do
        pct=$(go test -cover ./internal/$pkg/... 2>/dev/null | grep -oE 'coverage: [0-9.]+%' | head -1 | grep -oE '[0-9.]+')
        printf '  internal/%-10s %s%%\n' "$pkg" "$pct"
        awk "BEGIN {exit !($pct < {{ safety }})}" && { printf '\033[31m  ✗ below safety floor of {{ safety }}%%\033[0m\n'; fail=1; }
    done
    [ "$fail" -eq 0 ] && printf '\033[32m✓ coverage floors met\033[0m\n'
    exit $fail

# run one package or one test:  just test-one ./internal/policy  or  just test-one ./... -run TestPolicy
[group('test')]
test-one *args:
    go test -v {{ args }}

# produce an HTML coverage report
# -coverpkg is needed for an honest number: the protocol tests in internal/app
# exercise internal/tools, and without it that work is not attributed anywhere.
[group('test')]
coverage:
    go test -coverpkg=./... -coverprofile=coverage.out -covermode=atomic ./...
    go tool cover -html=coverage.out -o coverage.html
    @go tool cover -func=coverage.out | tail -1
    @printf '\033[32m✓ coverage.html written\033[0m\n'

# the security-relevant tests only, for a quick check after touching the safety layer
[group('test')]
test-safety:
    go test ./internal/policy/... ./internal/confirm/... ./internal/redact/... ./internal/untrusted/... ./internal/shape/... ./internal/app/...

# ── quality ──────────────────────────────────────────────────────────────────

# gofmt, go vet and golangci-lint
[group('quality')]
lint: fmt-check vet
    @command -v golangci-lint >/dev/null || (printf '\033[33m! golangci-lint not installed, run `just setup-tools`\033[0m\n'; exit 1)
    golangci-lint run

# format all Go sources in place
[group('quality')]
fmt:
    gofmt -s -w .
    @printf '\033[32m✓ formatted\033[0m\n'

# fail if anything is unformatted
[group('quality')]
fmt-check:
    @unformatted="$(gofmt -s -l .)"; \
    if [ -n "$unformatted" ]; then \
        printf '\033[31m✗ unformatted files:\033[0m\n%s\n' "$unformatted"; exit 1; \
    fi

# go vet
[group('quality')]
vet:
    go vet ./...

# tidy go.mod and go.sum
[group('quality')]
tidy:
    go mod tidy

# report known vulnerabilities in dependencies
[group('quality')]
audit:
    go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

# static application security testing
[group('quality')]
sast:
    @command -v gosec >/dev/null || (printf '\033[33m! gosec not installed, run `just setup-tools`\033[0m\n'; exit 1)
    gosec -quiet ./...
    @printf '\033[32m✓ no gosec findings\033[0m\n'

# lint the GitHub Actions workflows
[group('quality')]
actionlint:
    @command -v actionlint >/dev/null || (printf '\033[33m! actionlint not installed, run `just setup-tools`\033[0m\n'; exit 1)
    actionlint
    @printf '\033[32m✓ workflows clean\033[0m\n'

# check that every GitHub Action is pinned to a commit SHA, not a tag
[group('quality')]
pin-check:
    #!/usr/bin/env bash
    set -euo pipefail
    unpinned=$(grep -rhoE '^\s*(- )?uses: [^ ]+' .github/workflows/ \
        | grep -vE 'uses: \./' \
        | grep -vE '@[0-9a-f]{40}' || true)
    if [ -n "$unpinned" ]; then
        printf '\033[31m✗ actions not pinned to a commit SHA:\033[0m\n%s\n' "$unpinned"
        printf '  A tag is mutable. Pin it: gh api repos/OWNER/REPO/git/ref/tags/TAG --jq .object.sha\n'
        exit 1
    fi
    printf '\033[32m✓ every action is SHA-pinned\033[0m\n'

# everything CI runs, in one command
[group('quality')]
ci: fmt-check vet tidy-check test-race coverage-gate actionlint pin-check
    @printf '\033[32m✓ ci checks passed\033[0m\n'

# fail if go.mod or go.sum would change
[group('quality')]
tidy-check:
    #!/usr/bin/env bash
    set -euo pipefail
    cp go.mod go.mod.bak && cp go.sum go.sum.bak
    trap 'mv go.mod.bak go.mod; mv go.sum.bak go.sum' EXIT
    go mod tidy
    diff -q go.mod go.mod.bak >/dev/null && diff -q go.sum go.sum.bak >/dev/null \
        || { printf '\033[31m✗ go.mod/go.sum are not tidy, run `just tidy`\033[0m\n'; exit 1; }

# ── run ──────────────────────────────────────────────────────────────────────

# run over stdio in the readonly profile (reads WHMCS_MCP_* from the environment)
[group('run')]
run *args:
    go run {{ pkg }} {{ args }}

# print the active profile and the tools this configuration would advertise
[group('run')]
tools *args:
    go run {{ pkg }} -print-tools {{ args }}

# verify connectivity and credentials against the configured WHMCS instance
[group('run')]
healthcheck:
    go run {{ pkg }} -healthcheck

# serve over streamable http on loopback
[group('run')]
serve-http addr="127.0.0.1:8080":
    go run {{ pkg }} -transport http -addr {{ addr }}

# ── specs ────────────────────────────────────────────────────────────────────

# show the OpenSpec change status
[group('specs')]
spec-status:
    openspec status --change add-whmcs-mcp-server

# validate the OpenSpec change
[group('specs')]
spec-validate:
    openspec validate add-whmcs-mcp-server

# list the specs
[group('specs')]
spec-list:
    openspec list --specs

# ── container ────────────────────────────────────────────────────────────────

# build the container image
[group('container')]
image-build tag=version:
    {{ container_cmd }} build \
        --build-arg VERSION={{ tag }} \
        --build-arg COMMIT={{ commit }} \
        -t {{ image }}:{{ tag }} .
    @printf '\033[32m✓ built\033[0m {{ image }}:{{ tag }}\n'

# push the image (use an immutable version tag, never latest)
[group('container')]
image-push tag=version:
    {{ container_cmd }} push {{ image }}:{{ tag }}

# print the image digest, which is what deployments should reference
[group('container')]
image-digest tag=version:
    @{{ container_cmd }} inspect --format '{{{{ index .RepoDigests 0 }}' {{ image }}:{{ tag }}

container_cmd := if `command -v docker >/dev/null 2>&1; echo $?` == "0" { "docker" } else { "podman" }

# ── release ──────────────────────────────────────────────────────────────────

# tag a release:  just release v1.2.0
[group('release')]
release tag:
    @git diff --quiet || (printf '\033[31m✗ working tree is dirty\033[0m\n'; exit 1)
    just ci
    git tag -a {{ tag }} -m "{{ tag }}"
    @printf '\033[32m✓ tagged {{ tag }}\033[0m  \033[2mpush with: git push origin {{ tag }}\033[0m\n'
