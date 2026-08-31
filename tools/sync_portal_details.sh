#!/usr/bin/env bash
# Push the storefront (docs/portal.md) and its metadata (tools/portal_meta.json)
# to the mod portal. Partial update: only the fields sent here change, so the
# gallery, title and release history are untouched.
#
# Usage: tools/sync_portal_details.sh [--dry-run]
# Env:   FACTORIO_API_KEY (required) — needs the "ModPortal: Edit Mods" scope,
#        which is separate from the "Upload Mods" scope used for releases.
#
# API: https://wiki.factorio.com/Mod_details_API  (POST /api/v2/mods/edit_details)
set -euo pipefail
cd "$(dirname "$0")/.."

command -v jq >/dev/null || { echo "jq required" >&2; exit 1; }

# House rules are enforced here, not in review — a relative link on the portal
# 404s silently and nobody notices for weeks.
python3 tools/portal_lint.py

META=tools/portal_meta.json
MOD=$(jq -r .mod "$META")

ARGS=( -F "mod=${MOD}" -F "description=<docs/portal.md" )
for field in summary category license homepage source_url; do
    value=$(jq -r --arg f "$field" '.[$f] // empty' "$META")
    [[ -n "$value" ]] && ARGS+=( --form-string "${field}=${value}" )
done
# tags is multi-valued: one repeated form field per tag.
while read -r tag; do
    [[ -n "$tag" ]] && ARGS+=( --form-string "tags=${tag}" )
done < <(jq -r '.tags[]? // empty' "$META")

if [[ "${1:-}" == "--dry-run" ]]; then
    printf 'would send:\n'; printf '  %s\n' "${ARGS[@]}"; exit 0
fi

: "${FACTORIO_API_KEY:?FACTORIO_API_KEY env var not set}"
RESP=$(curl -sS -w $'\nHTTP_CODE:%{http_code}' \
    -H "Authorization: Bearer ${FACTORIO_API_KEY}" \
    "${ARGS[@]}" \
    "https://mods.factorio.com/api/v2/mods/edit_details")

HTTP=$(echo "$RESP" | tail -n1 | cut -d: -f2)
BODY=$(echo "$RESP" | sed '$d')
echo "edit_details HTTP ${HTTP}"
echo "$BODY"

if [[ "$HTTP" -lt 200 || "$HTTP" -ge 300 ]]; then
    echo "edit_details failed — a 403 usually means the API key lacks the" >&2
    echo "'ModPortal: Edit Mods' scope (the release key only has Upload)." >&2
    exit 1
fi
[[ "$(echo "$BODY" | jq -r '.success // false')" == "true" ]] \
    || { echo "edit_details returned non-success" >&2; exit 1; }
echo "portal details synced for ${MOD}"
