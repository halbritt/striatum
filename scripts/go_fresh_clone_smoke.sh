#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/scripts/smoke_common.sh"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

CLONE="$TMP/striatum"
mkdir -p "$CLONE"
tar \
  --exclude .git \
  --exclude .striatum \
  --exclude build \
  --exclude dist \
  -C "$ROOT" \
  -cf - \
  . | tar -C "$CLONE" -xf -

VERSION="$(tr -d '[:space:]' < "$CLONE/VERSION")"
TRIPLE="$(smoke_host_triple)"
DIST="$TMP/dist"

"$CLONE/scripts/build_go_release_archives.sh" --dist "$DIST" --target "$TRIPLE" >/dev/null
"$CLONE/scripts/check_go_release_archives.sh" "$DIST" >/dev/null

archive="$(smoke_find_archive "$DIST" "$VERSION" "$TRIPLE")"
release_root="$(smoke_extract_archive "$archive" "$TMP/release")"

"$release_root/bin/striatum" --help >/dev/null
(
  cd "$CLONE"
  "$release_root/bin/striatum" workflow validate \
    --allow-same-model-pairing \
    --json \
    docs/operator/workflows/rfc-0078-go-only-packaging-release.json >/dev/null
)
"$release_root/bin/striatumd" --describe | grep -F "core=go" >/dev/null
smoke_note_postgres_skip

echo "go fresh clone smoke: ok"
