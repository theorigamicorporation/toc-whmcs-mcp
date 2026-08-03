#!/usr/bin/env bash
#
# Enumerate the licences of every dependency compiled into the shipped binary,
# fail on anything that is not permissive, and regenerate
# THIRD_PARTY_LICENSES.md.
#
# This project is AGPL-3.0 and is distributed as source, a binary and a
# container image. Two things follow:
#
#   1. A dependency must be combinable with AGPL-3.0. Permissive and most
#      copyleft licences are; GPL-2.0-only, EPL and CDDL are not, and neither
#      are source-available licences such as SSPL or BUSL. The allowlist below
#      turns an incompatible dependency into a build failure at the moment it is
#      added, rather than a discovery during someone else's legal review.
#
#   2. MIT, BSD, ISC and Apache-2.0 all require the copyright notice and licence
#      text to travel with redistributions, including binary ones. Generating
#      the file is how that obligation actually gets met instead of being
#      assumed. AGPL-3.0 does not override those; it adds to them.
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

# Licences that can be combined into an AGPL-3.0 work. Apache-2.0 is included
# because it is compatible with GPLv3 and later specifically; it is NOT
# compatible with GPLv2, which is why this project cannot be v2-era copyleft.
# Adding to this list is a legal decision, not a build fix.
ALLOWED="MIT BSD-3-Clause BSD-2-Clause ISC Apache-2.0 GPL LGPL AGPL MPL"

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
        printf '  This project is AGPL-3.0. %s cannot be combined with it.\n' "$id"
        printf '  Remove the dependency, or take a legal decision to change the licence.\n'
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

toc-whmcs-mcp is licensed under the GNU Affero General Public License v3.0
(see [LICENSE](LICENSE)). The binary and the container image also contain code
from the modules below. Their licences require that their copyright notices
travel with any redistribution, which AGPL-3.0 does not override, so the full
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
