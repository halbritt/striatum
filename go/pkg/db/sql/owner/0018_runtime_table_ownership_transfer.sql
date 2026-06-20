-- GH #442 (also resolves #441) — owner bundle 0018: transfer the pre-split
-- runtime-table cohort to striatumd_rw so runtime migrations may ALTER them.
--
-- Applied OUT-OF-BAND as the database owner via `striatum daemon owner-ddl
-- apply --owner-url …`, BEFORE the daemon's runtime self-bootstrap runs the
-- runtime migrations (the deploy runbook ordering, docs/how-to/postgres-transition.md).
--
-- THE BUG THIS CLOSES. On a two-role production deploy (RFC 0110) the schema
-- migrations sql/NNNN_*.sql are applied by the OWNER role through
-- `daemon migrate-db --admin-url …` (the runbook's fresh-bootstrap step), so a
-- table a runtime migration CREATEs (e.g. striatumd.job_recovery_state, created
-- by migration 0020) is owned by the bootstrap/owner role (halbritt), NOT the
-- runtime role striatumd_rw. The runtime role has DML on these tables but not
-- ownership. When a LATER runtime migration ALTERs such a table — migration
-- 0035 (RFC 0131 131-C, #336) adds three confidence-gate columns to
-- job_recovery_state — and that migration runs under the runtime role (the
-- daemon's own ConnectAndMigrate self-bootstrap applies pending migrations as
-- striatumd_rw on every restart), PostgreSQL refuses with `must be owner of
-- table job_recovery_state` (SQLSTATE 42501). The daemon exits before serving →
-- crash-loop on every restart under systemd Restart=on-failure (observed in
-- prod, #441/#442). pgtest never caught it: pgtest is single-role, so the test
-- connector owns its tables and the ALTER succeeds there.
--
-- THE FIX. Transfer ownership of the at-risk pre-split cohort to striatumd_rw
-- here, in an owner bundle, applied as the owner BEFORE the runtime migrations.
-- After the transfer the table is genuinely runtime-owned, so the runtime role
-- can ALTER it: migration 0035's ADD COLUMN succeeds under striatumd_rw without
-- the owner-only crash. This is the SAME mechanism owner bundle 0006 (RFC 0114 /
-- D173) used to move principals/principal_clients/client_sessions ownership —
-- here in the opposite direction (toward the runtime role) because these tables
-- are runtime DATA the runtime role must own, not owner-gated identity surfaces.
--
-- THE COHORT. Every striatumd.* table CREATEd by a runtime migration (sql/NNNN)
-- whose ownership on a two-role deploy lands on the owner role and which a
-- runtime migration above the floor-27 owner-DDL guard may ALTER. The two
-- known live cases are job_recovery_state (ALTERed by 0035) and
-- barrier_staged_contributions (ALTERed by 0036, RFC 0133 base-drift leg). The
-- remaining tables in the cohort are the sibling pre-split runtime-data tables
-- named on #441 — transferred prophylactically so a FUTURE above-floor runtime
-- ALTER of any of them is safe by construction, never a fresh crash-loop. These
-- carry runtime DATA only (recovery budgets, barrier journal, conversation /
-- interrogation / dissent ledgers, plain-dir workspaces, spawn grants); none is
-- an authority-gated owner surface, so moving ownership to the runtime role does
-- NOT weaken any RFC 0110 read/write boundary (those boundaries are the
-- owner-owned SECURITY DEFINER functions + the revokes in bundles 0003-0006,
-- which this bundle does not touch).
--
-- IDEMPOTENT + DEPLOY-ORDER SAFE. ALTER TABLE … OWNER TO is a no-op when the
-- table is already owned by striatumd_rw, so a re-run — or applying onto a DB an
-- operator already hand-reassigned (prod did this to recover, #441) — is safe.
-- It is guarded by an EXISTS-striatumd_rw probe (a single-role deploy that never
-- provisioned the runtime role is left untouched) and a to_regclass probe per
-- table (a deploy behind on the runtime migration that created the table skips
-- it). In pgtest (single-role) the per-test pool role both owns the tables and
-- is a member of the cluster striatumd_rw role, so the transfer is a real
-- ownership move to the shared parent role and is a no-op on re-run.
--
-- PREREQUISITES, both satisfied by this bundle (applied as the owner):
--   1. The owner role MUST be a member of striatumd_rw — PostgreSQL requires the
--      role running ALTER … OWNER TO to be a member of the new owning role. The
--      two-role bootstrap grants this (`GRANT striatumd_rw TO <owner-role>`,
--      docs/how-to/postgres-transition.md); pgtest grants it in ensureRuntimeRole.
--      If absent the ALTER raises a clear `must be member of role "striatumd_rw"`.
--   2. striatumd_rw MUST hold CREATE on schema striatumd — PostgreSQL requires the
--      NEW owner of a table to be able to create objects in the table's schema, so
--      ALTER … OWNER TO striatumd_rw fails `permission denied for schema striatumd`
--      without it (confirmed live). Production already grants this (the daemon's
--      runtime role owns/created runtime tables there), but the documented runbook
--      setup + migration 0005 grant striatumd_rw only USAGE + table DML, so a fresh
--      deploy that followed only the runbook would NOT have it. This bundle GRANTs
--      it idempotently FIRST so the transfer is self-contained and works on a fresh
--      runbook deploy without an out-of-band manual grant. CREATE on the schema is
--      the minimum privilege the runtime role needs to OWN these runtime-data
--      tables; it does not grant any authority over the owner-held SECURITY DEFINER
--      functions or the gated identity/audit surfaces (those stay owner-owned).

DO $$
DECLARE
  v_table text;
  v_cohort text[] := ARRAY[
    'job_recovery_state',             -- migration 0020; ALTERed by 0035 (the live crash)
    'barrier_staged_contributions',   -- migration 0029; ALTERed by 0036 (base-drift leg)
    'barrier_state',                  -- migration 0030
    'fanin_freeze_points',            -- migration 0029
    'conversations',                  -- migration 0017
    'conversation_post_dialog_hooks', -- migration 0034
    'dissent_ledger',                 -- migration 0032
    'interrogations',                 -- migration 0016
    'job_workspaces',                 -- migration 0028
    'spawn_authorization_grants'      -- migration 0027
  ];
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'striatumd_rw') THEN
    -- Single-role deploy: no runtime role to own these. Nothing to transfer.
    RETURN;
  END IF;
  -- Prerequisite #2: the runtime role must be able to own objects in the schema.
  GRANT CREATE ON SCHEMA striatumd TO striatumd_rw;
  FOREACH v_table IN ARRAY v_cohort LOOP
    IF to_regclass('striatumd.' || v_table) IS NOT NULL THEN
      EXECUTE format(
        'ALTER TABLE striatumd.%I OWNER TO striatumd_rw', v_table);
    END IF;
  END LOOP;
END;
$$;

INSERT INTO striatumd.schema_authority(capability, requires_daemon_auth, bundle_version)
VALUES ('runtime_table_ownership_transfer', false, 18)
ON CONFLICT (capability) DO NOTHING;
