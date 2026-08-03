#!/usr/bin/env bash
#
# Generate the README status badges as SVG files under docs/badges/.
#
# They are committed and referenced by relative path rather than fetched from
# shields.io or a gh-pages branch. Both of those need anonymous read access to
# work, and this repository is private, so they would render as broken images
# for exactly the people who are allowed to see them.
#
# Usage: just badges
set -euo pipefail

cd "$(dirname "$0")/.."
out="docs/badges"
mkdir -p "$out"

# svg writes one flat-square badge. Widths are computed from character counts,
# which is approximate but stable: the point is a legible badge, not pixel
# parity with shields.io.
svg() {
    local file="$1" label="$2" message="$3" colour="$4"
    local label_w message_w total
    label_w=$(( ${#label} * 7 + 12 ))
    message_w=$(( ${#message} * 7 + 12 ))
    total=$(( label_w + message_w ))

    cat > "$out/$file" <<SVG
<svg xmlns="http://www.w3.org/2000/svg" width="$total" height="20" role="img" aria-label="$label: $message">
  <title>$label: $message</title>
  <g shape-rendering="crispEdges">
    <rect width="$label_w" height="20" fill="#555"/>
    <rect x="$label_w" width="$message_w" height="20" fill="$colour"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">
    <text x="$(( label_w / 2 ))" y="14">$label</text>
    <text x="$(( label_w + message_w / 2 ))" y="14">$message</text>
  </g>
</svg>
SVG
}

# --- OpenSpec ---------------------------------------------------------------

specs_json=$(openspec list --specs --json)
specs=$(echo "$specs_json" | grep -c '"id"' || true)
requirements=$(echo "$specs_json" | grep -oE '"requirementCount": *[0-9]+' | grep -oE '[0-9]+' | awk '{s+=$1} END {print s+0}')
changes=$(openspec list --json | grep -c '"name"' || true)

svg specs.svg "specs" "$specs" "#0a7bbb"
svg requirements.svg "requirements" "$requirements" "#0a7bbb"
svg open-changes.svg "open changes" "$changes" "$([ "$changes" -eq 0 ] && echo '#3fb950' || echo '#d29922')"

# --- Code -------------------------------------------------------------------

actions=$(grep -oE 'GeneratedActionCount = [0-9]+' internal/registry/actions_gen.go | grep -oE '[0-9]+')
svg actions.svg "WHMCS actions" "$actions" "#0a7bbb"

go_version=$(grep -oE '^golang [0-9.]+' .tool-versions | cut -d' ' -f2)
svg go.svg "go" "$go_version" "#00ADD8"

# Coverage, when a profile is present. Never invent a number: if nobody has run
# the tests, the badge says so rather than showing a stale figure.
if [ -f coverage.out ]; then
    coverage=$(go tool cover -func=coverage.out | grep '^total:' | awk '{print $3}')
    pct=${coverage%\%}
    if awk "BEGIN {exit !($pct >= 80)}"; then
        colour="#3fb950"
    elif awk "BEGIN {exit !($pct >= 65)}"; then
        colour="#d29922"
    else
        colour="#f85149"
    fi
    svg coverage.svg "coverage" "$coverage" "$colour"
else
    svg coverage.svg "coverage" "unknown" "#8b949e"
fi

printf '\033[32m✓ badges written to %s\033[0m\n' "$out"
ls -1 "$out"
