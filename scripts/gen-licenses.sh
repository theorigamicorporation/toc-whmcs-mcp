#!/usr/bin/env bash
#
# Enumerate the licences of every dependency compiled into the shipped binary,
# fail on anything that is not permissive, and regenerate
# THIRD_PARTY_LICENSES.md.
#
# This project is proprietary and is distributed as a binary and a container
# image. Two things follow:
#
#   1. A copyleft dependency (GPL, LGPL, AGPL, MPL, EPL, CDDL) would impose
#      source-disclosure or file-level reciprocity obligations that conflict
#      with shipping it closed. The allowlist below turns that into a build
#      failure at the moment the dependency is added, rather than a discovery
#      during a customer's legal review.
#
#   2. MIT, BSD, ISC and Apache-2.0 all require the copyright notice and licence
#      text to travel with binary redistributions. Generating the file is how
#      that obligation actually gets met instead of being assumed.
#
# Only modules linked into cmd/toc-whmcs-mcp are considered. Test-only and
# development-only dependencies (testify, x/net used by cmd/docgen, the linters)
# are not distributed, so they carry no notice obligation.
#
# Usage: just licenses        regenerate and check
#        just licenses-check  check only, no write (CI)
set -euo pipefail

cd "$(dirname "$0")/.."

check_only="${1:-}"
out="THIRD_PARTY_LICENSES.md"
module_path="github.com/theorigamicorporation/toc-whmcs-mcp"

# Licences compatible with shipping a proprietary binary, provided the notice
# travels with it. Adding to this list is a legal decision, not a build fix.
ALLOWED="MIT BSD-3-Clause BSD-2-Clause ISC Apache-2.0"

# modcache maps a module path to its cache directory. Uppercase letters are
# escaped as !lowercase, which is how the Go module cache avoids collisions on
# case-insensitive filesystems.
modcache() {
    local mod="$1" ver="$2"
    printf '%s/pkg/mod/%s@%s' "$(go env GOPATH)" \
        "$(echo "$mod" | sed 's/\([A-Z]\)/!\l\1/g')" "$ver"
}

# classify identifies a licence from its text. Order matters: the BSD variants
# share their opening paragraph and are told apart by the third clause.
classify() {
    local file="$1" text
    text=$(tr -s '[:space:]' ' ' < "$file")

    case "$text" in
        *"GNU AFFERO GENERAL PUBLIC LICENSE"*)             echo "AGPL"; return ;;
        *"GNU LESSER GENERAL PUBLIC LICENSE"*)             echo "LGPL"; return ;;
        *"GNU GENERAL PUBLIC LICENSE"*)                    echo "GPL"; return ;;
        *"Mozilla Public License"*)                        echo "MPL"; return ;;
        *"Eclipse Public License"*)                        echo "EPL"; return ;;
        *"COMMON DEVELOPMENT AND DISTRIBUTION LICENSE"*)   echo "CDDL"; return ;;
        *"Apache License"*"Version 2.0"*)                  echo "Apache-2.0"; return ;;
        *"Permission to use, copy, modify, and/or distribute"*) echo "ISC"; return ;;
        *"Permission is hereby granted, free of charge"*)  echo "MIT"; return ;;
    esac

    case "$text" in
        *"Redistribution and use in source and binary forms"*)
            case "$text" in
                *"Neither the name of"*|*"names of its contributors"*)
                    echo "BSD-3-Clause" ;;
                *)  echo "BSD-2-Clause" ;;
            esac
            return ;;
    esac

    echo "UNKNOWN"
}

# --- collect ----------------------------------------------------------------

# Modules whose code is actually linked into the binary we ship.
mapfile -t shipped < <(
    go list -deps ./cmd/toc-whmcs-mcp \
        | xargs -r go list -f '{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{end}}' \
        | grep -v "^$module_path" | grep -v '^ *$' | sort -u
)

if [ "${#shipped[@]}" -eq 0 ]; then
    echo "no dependencies resolved; is the module tidy?" >&2
    exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail=0
: > "$tmp/rows"

for entry in "${shipped[@]}"; do
    mod="${entry%% *}"
    ver="${entry##* }"
    dir=$(modcache "$mod" "$ver")

    licfile=$(find "$dir" -maxdepth 1 -iregex '.*/\(LICENSE\|LICENCE\|COPYING\)\(\.md\|\.txt\)?' 2>/dev/null | head -1)
    if [ -z "$licfile" ]; then
        printf '\033[31m✗ %s has no licence file\033[0m\n' "$mod"
        fail=1
        continue
    fi

    id=$(classify "$licfile")
    if ! grep -qw -- "$id" <<< "$ALLOWED"; then
        printf '\033[31m✗ %s is %s, which is not on the allowlist\033[0m\n' "$mod" "$id"
        printf '  This project ships as a proprietary binary. %s imposes obligations\n' "$id"
        printf '  that conflict with that. Remove the dependency or take a legal decision.\n'
        fail=1
        continue
    fi

    printf '%s\t%s\t%s\t%s\n' "$mod" "$ver" "$id" "$licfile" >> "$tmp/rows"
    printf '  %-45s %-12s %s\n' "$mod" "$ver" "$id"
done

[ "$fail" -eq 0 ] || exit 1

# --- render -----------------------------------------------------------------

{
    cat <<'HEADER'
# Third-party licences

toc-whmcs-mcp is proprietary (see [LICENSE](LICENSE)), but the binary and the
container image contain code from the open-source modules below. Their licences
require that their copyright notices travel with any redistribution, so the full
text of each is reproduced here.

This file is generated by `scripts/gen-licenses.sh`. Do not edit it by hand; run
`just licenses`. CI fails if it is stale, or if a dependency arrives under a
licence that is not permissive.

Only modules linked into the shipped binary are listed. Test-only and
development-only dependencies are not distributed and carry no notice
obligation.

HEADER

    printf '## Summary\n\n| Module | Version | Licence |\n| --- | --- | --- |\n'
    while IFS=$'\t' read -r mod ver id _; do
        printf '| `%s` | %s | %s |\n' "$mod" "$ver" "$id"
    done < "$tmp/rows"

    printf '\nThe Go standard library is BSD-3-Clause, Copyright (c) 2009 The Go Authors.\n'
    printf '\n---\n\n## Full licence texts\n'

    while IFS=$'\t' read -r mod ver id licfile; do
        printf '\n### %s\n\n_%s, version %s_\n\n```\n' "$mod" "$id" "$ver"
        cat "$licfile"
        printf '```\n'
    done < "$tmp/rows"
} > "$tmp/out.md"

if [ "$check_only" = "--check" ]; then
    if ! diff -q "$tmp/out.md" "$out" >/dev/null 2>&1; then
        printf '\033[31m✗ %s is stale; run `just licenses` and commit the result\033[0m\n' "$out"
        diff -u "$out" "$tmp/out.md" | head -40 || true
        exit 1
    fi
    printf '\033[32m✓ %s is up to date, all %d dependencies permissive\033[0m\n' "$out" "$(wc -l < "$tmp/rows")"
else
    mv "$tmp/out.md" "$out"
    printf '\033[32m✓ %s written, all %d dependencies permissive\033[0m\n' "$out" "$(wc -l < "$tmp/rows")"
fi
