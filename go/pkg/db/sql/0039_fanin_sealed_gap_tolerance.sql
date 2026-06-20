-- RFC 0138 (#453) — opt-in terminal-gap tolerance for a STRICT fan-in barrier with
-- a permanently-unrecoverable REQUIRED seat. This is the missing analog of the
-- quorum abstention budget (RFC 0132 / D214b), applied to the fan-in instance of
-- the sealed expectation barrier (RFC 0135 / entity=job, seal=attempt) WITHOUT
-- silently forging completeness.
--
-- The default (opt-in OFF) is RFC 0138 Option A exactly: a strict fan-in's required
-- seats are required, so a permanently-dead one stops and asks a human
-- (needs_operator) — no automatic degraded fire. The columns added here are FALSE/0
-- by default, so every existing strict fan-in is unchanged.
--
-- When a barrier explicitly opts in (fanin_tolerates_sealed_gap = true,
-- max_sealed_gaps > 0), at most max_sealed_gaps PROVABLY-DEAD required seats
-- (supervisedAgentConfirmedDead, the SAME forgery-resistant oracle the quorum path
-- reuses — never a new liveness check, never a merely-slow seat) may be admitted as
-- is_terminal_gap in-edges so the barrier fires DEGRADED. The degraded fire records
-- each gap in the join_manifest.v1 with status=terminal_gap + a damage_code, so a
-- downstream gate can refuse a short join. Completeness is never silently forged.
--
-- WHY THE FREEZE RECORD (not just workflow config). The tolerance is SEALED with the
-- barrier at fan-out: the freeze record (striatumd.fanin_freeze_points) is the
-- immutable, append-only declaration of WHICH seats are required and WHAT base they
-- fold onto. Sealing the tolerance with the same record makes "this barrier may fire
-- short of N required legs" a durable, auditable property of the barrier itself
-- rather than a value re-read (and re-interpretable) from live workflow config — the
-- author's opt-in is frozen alongside the seats it relaxes.
--
-- Ownership-safety (D215 / RFC 0079 §5 / the migration owner-table trap, and the
-- 0036_barrier_base_drift_leg.sql precedent): striatumd.fanin_freeze_points is a
-- RUNTIME-owned table (created in migration 0029, owned by striatumd_rw) and carries
-- NO foreign key into the owner-held jobs table. This migration ADDs columns to that
-- runtime-owned table with ADD COLUMN IF NOT EXISTS — it does NOT ALTER or DROP any
-- OWNER-held table, so the future-runtime-DDL guard
-- (TestFutureRuntimeMigrationsDoNotCarryOwnerDDL) is satisfied and it applies cleanly
-- as striatumd_rw without crash-looping a two-role production daemon (D187 / #244).
-- A deployment behind on this migration falls back to today's behaviour (no sealed
-- gap admitted — every strict fan-in parks in needs_operator on a dead required
-- seat), which is degrade-safe, never silent corruption. Owner-applied at deploy +
-- substrate_version bumped to 39.
--
-- NOTE: the freeze table is append-only (SELECT/INSERT grant + refuse-trigger from
-- migration 0029). ADD COLUMN with a constant DEFAULT on an existing append-only
-- table is fine — it is a one-time schema change applied by the migration runner (a
-- privileged role), not a runtime UPDATE; the columns are populated at INSERT time
-- (fan-out) and never mutated thereafter, preserving the freeze record's
-- immutability.

-- The per-barrier opt-in: does this strict fan-in TOLERATE firing degraded over a
-- sealed terminal gap (a provably-dead required seat)? Default FALSE — Option A: a
-- dead required seat parks the run in needs_operator. No existing barrier is affected.
ALTER TABLE striatumd.fanin_freeze_points
  ADD COLUMN IF NOT EXISTS fanin_tolerates_sealed_gap boolean NOT NULL DEFAULT false;

-- The bounded budget: HOW MANY required legs the join may be short (the fan-in analog
-- of the quorum's max_gating_abstentions). Default 0 — even with the boolean opt-in
-- true, a 0 budget admits no gap, so the opt-in is fully expressed only by a positive
-- budget. A provably-dead seat BEYOND the budget stays blocking (mirrors
-- resolvePanelSeats' beyond-budget rule), so the run still parks rather than firing a
-- join shorter than the author declared tolerable.
ALTER TABLE striatumd.fanin_freeze_points
  ADD COLUMN IF NOT EXISTS max_sealed_gaps integer NOT NULL DEFAULT 0;

-- ADD COLUMN inherits the table's existing grants, so the runtime role already has
-- SELECT/INSERT on the new columns. Re-assert the append-only grant idempotently
-- anyway (own GRANT per D215, matching the pattern migration 0029 established for this
-- table) so the migration is self-contained — harmless if the role or grant already
-- exists.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'striatumd_rw') THEN
    GRANT SELECT, INSERT ON striatumd.fanin_freeze_points TO striatumd_rw;
  END IF;
END;
$$;
