-- RFC 0141 / D243 (#482): daemon-owned operator attestations — the gate-enforced
-- PINNED → VERIFIABLE trust boundary.
--
-- RFC 0141 (D238/D239) shipped a generatable verification_gate at EXPERIMENTAL
-- tier with a documented forge-able gap: the PINNED → VERIFIABLE attestation hinge
-- was enforced ONLY at the `striatum verifier attest` VERB level (it refuses a
-- lane/session context and writes a per-host `allowlist.pins.<fp>.attest.json`
-- sidecar). The run-completion gate (evaluateRunClaimVerification) never re-verified
-- attestation — so a non-compliant lane could write a FORGED sidecar, join →
-- VERIFIABLE, run the real pinned external check, author the claim VERIFIED, and the
-- daemon gate would honor it. Attestation was lane-side honesty, not a gate-enforced
-- trust boundary.
--
-- This table moves the authoritative attestation into the daemon-owned PG live state
-- (the product boundary, RFC 0033 + D094 / RFC 0043). It is minted by the
-- operator-token `verifier.attest` RPC (CapabilityAdmin, REFUSED for any
-- session-bound token — the verified lane can never bless its own pins) and READ by
-- the run-completion gate, which now REFUSES VERIFIED for an external (non-builtin)
-- claim whose backing check lacks an un-revoked attestation row binding
-- (repository_id, check_id, binary_sha256). The repo-file sidecar becomes a
-- cache/projection, no longer the trust source. Fail-closed: a missing/revoked row
-- caps the claim at ASSERTED.
--
-- An attestation binds the EXACT pinned sha (binary_sha256), so a later re-pin of
-- different bytes does NOT inherit the blessing — the gate read of a receipt sealed
-- to the new bytes finds no matching row and decays to ASSERTED.
--
-- Ownership-safety (RFC 0079 §5 / the migration owner-table trap): like
-- 0027_spawn_authorization_grants.sql and 0023_principals.sql, this migration ONLY
-- creates a NEW table (which the runtime role owns) and NEVER alters an owner-held
-- table, so it applies cleanly as striatumd_rw and cannot crash-loop the daemon. It
-- is a runtime migration (not an owner bundle) because the run-completion gate reads
-- it on EVERY terminal transition: the table must exist for every daemon from first
-- boot, or those read paths would error against a table that does not exist until
-- `owner-ddl apply`.
--
-- attested_by is a bare text column with NO foreign key to striatumd.principals —
-- mirroring spawn_authorization_grants.owner_principal_id / audit_log.client_id
-- (referential integrity enforced in Go; a FK REFERENCES on an owner-gated table is
-- exactly the trap). It records the operator principal the daemon authorized the
-- attestation against.
--
-- The GRANT block makes the table writable+readable by the runtime role (broad-DML
-- migration 0005 ran before this table existed, so a fresh table needs its own
-- grant; pgtest masks a missing GRANT, so it is mandatory).

CREATE TABLE IF NOT EXISTS striatumd.verifier_attestations (
  repository_id  text NOT NULL,
  attestation_id text PRIMARY KEY,
  check_id       text NOT NULL,
  binary_sha256  text NOT NULL,
  attested_by    text,
  attested_at    timestamptz NOT NULL DEFAULT now(),
  revoked_at     timestamptz,
  revoke_reason  text
);

-- At most one ACTIVE (un-revoked) attestation per (repo, check, exact bytes): the
-- gate resolves "is THIS check at THIS sha attested in THIS repo" unambiguously, and
-- a re-attest cannot stack two live rows for the same bytes.
CREATE UNIQUE INDEX IF NOT EXISTS uq_striatumd_verifier_attestations_active
  ON striatumd.verifier_attestations (repository_id, check_id, binary_sha256)
  WHERE revoked_at IS NULL;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'striatumd_rw') THEN
    GRANT SELECT, INSERT, UPDATE ON striatumd.verifier_attestations TO striatumd_rw;
  END IF;
END
$$;
