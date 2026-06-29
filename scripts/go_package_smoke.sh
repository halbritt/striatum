#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/scripts/smoke_common.sh"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
TRIPLE="$(smoke_host_triple)"
DIST="$TMP/dist"

"$ROOT/scripts/build_go_release_archives.sh" --dist "$DIST" --target "$TRIPLE" >/dev/null
"$ROOT/scripts/check_go_release_archives.sh" "$DIST" >/dev/null

archive="$(smoke_find_archive "$DIST" "$VERSION" "$TRIPLE")"
release_root="$(smoke_extract_archive "$archive" "$TMP/release")"

"$release_root/bin/striatum" workflow validate \
  --allow-same-model-pairing \
  --json \
  "$ROOT/docs/operator/workflows/rfc-0078-go-only-packaging-release.json" >/dev/null

"$release_root/bin/striatumd" --describe | grep -F "core=go" >/dev/null
"$release_root/bin/striatumd" --describe | grep -E "migration_count=[1-9][0-9]*" >/dev/null
smoke_note_postgres_skip

echo "go package smoke: ok"
