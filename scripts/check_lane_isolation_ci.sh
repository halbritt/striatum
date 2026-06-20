#!/usr/bin/env bash
set -euo pipefail

# RFC 0096 / #87 lane-isolation gate — CI / operator wrapper (D244).
#
# Disposition: the host-isolation gate is OPERATOR-PROVISIONED HARDENING, not a
# mandatory-in-CI check. The underlying negative control
# (scripts/check_lane_isolation_neg.sh, the RFC 0110 T-LANE-ISOLATION-NEG gate)
# fundamentally requires a host that has been provisioned per
# docs/how-to/lane-sandbox.md: a dedicated PG-less lane OS user, passwordless
# `sudo -n -u <lane-user>`, and PostgreSQL `pg_hba.conf` reject rules for that
# user. A stock GitHub Actions runner has none of these, so the gate cannot be
# made unconditionally mandatory without breaking CI or — worse — passing
# vacuously on a host where the lane user simply does not exist.
#
# This wrapper makes the disposition legible and the skip LOUD:
#
#   * STRIATUM_LANE_ISOLATION_HOST=1  -> the host advertises provisioning; run
#     the REAL negative control. If the probe URLs / lane user / sudo are not
#     actually in place it FAILS LOUDLY (it must never skip silently once a host
#     claims to be provisioned).
#   * otherwise                       -> print a clear SKIPPED message and exit
#     0, so a green CI run never falsely implies the isolation gate ran.

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

skip() {
  cat >&2 <<MSG
SKIPPED: host not provisioned for lane-isolation gate
  (operator-provisioned hardening, see D244 + docs/how-to/lane-sandbox.md)

  The RFC 0096 / #87 lane-isolation negative control needs a dedicated PG-less
  lane OS user, passwordless 'sudo -n -u <lane-user>', and PostgreSQL pg_hba
  reject rules. Stock CI runners do not have these, so this gate is run only on
  a host that advertises provisioning.

  To RUN this gate (on a provisioned host), set:
    STRIATUM_LANE_ISOLATION_HOST=1
    STRIATUM_LANE_OS_USER=<lane-user>
    STRIATUM_LANE_ISOLATION_UNIX_URL=<protected-unix-socket-postgres-url>
    STRIATUM_LANE_ISOLATION_TCP_URL=<loopback-tcp-postgres-url>
MSG
  echo "lane-isolation-check: SKIPPED (host not provisioned; gate did NOT run)"
  exit 0
}

if [[ "${STRIATUM_LANE_ISOLATION_HOST:-}" != "1" ]]; then
  skip
fi

echo "STRIATUM_LANE_ISOLATION_HOST=1: host advertises lane-isolation provisioning; running the real negative control (D244)." >&2

# Once a host CLAIMS provisioning, refuse to skip silently: a missing probe URL
# is now a configuration error, not a reason to pass. check_lane_isolation_neg.sh
# already exits 2 on missing env / missing lane user / missing psql, which we
# propagate as a real failure.
exec "$here/check_lane_isolation_neg.sh"
