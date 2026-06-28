#!/usr/bin/env bash

smoke_host_triple() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$os" in
    linux) os="linux" ;;
    darwin) os="darwin" ;;
    *) echo "unsupported smoke OS: $os" >&2; return 1 ;;
  esac
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "unsupported smoke architecture: $arch" >&2; return 1 ;;
  esac
  printf '%s-%s\n' "$os" "$arch"
}

smoke_sha256() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$@"
  else
    sha256sum "$@"
  fi
}

smoke_find_archive() {
  local dist="$1"
  local version="$2"
  local triple="$3"
  local archive="$dist/striatum_${version}_${triple}.tar.gz"
  test -f "$archive" || {
    echo "missing release archive: $archive" >&2
    return 1
  }
  printf '%s\n' "$archive"
}

smoke_extract_archive() {
  local archive="$1"
  local dest="$2"
  mkdir -p "$dest"
  tar -xzf "$archive" -C "$dest"
  find "$dest" -mindepth 1 -maxdepth 1 -type d -print -quit
}

smoke_note_postgres_skip() {
  if [[ -z "${STRIATUM_DAEMON_DB_URL:-}" ]]; then
    echo "smoke: PostgreSQL integration skipped; STRIATUM_DAEMON_DB_URL is not set" >&2
    return 0
  fi
  if ! command -v psql >/dev/null 2>&1; then
    echo "smoke: PostgreSQL integration skipped; psql is not available" >&2
    return 0
  fi
  local psql_error
  if ! psql_error="$(psql "$STRIATUM_DAEMON_DB_URL" -c 'select 1' 2>&1 >/dev/null)"; then
    case "$psql_error" in
      *"password authentication failed"*|*"no pg_hba.conf entry"*|*"authentication failed"*)
        echo "smoke: PostgreSQL integration skipped; direct psql authentication failed" >&2
        ;;
      *"Connection refused"*|*"connection refused"*|*"could not connect"*|*"timeout expired"*|*"No route to host"*)
        echo "smoke: PostgreSQL integration skipped; configured database is not reachable by direct psql probe" >&2
        ;;
      *)
        echo "smoke: PostgreSQL integration skipped; direct psql probe failed" >&2
        ;;
    esac
    return 0
  fi
  echo "smoke: PostgreSQL is reachable; full daemon lifecycle smoke belongs to the daemon integration gate" >&2
}
