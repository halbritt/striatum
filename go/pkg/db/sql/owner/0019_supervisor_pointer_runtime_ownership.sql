-- GH #421 / RFC 0139 — owner bundle 0019: transfer the supervisor-pointer table
-- cohort to striatumd_rw so the runtime migration 0039 index reshape (drop `state`
-- from idx_process_supervisor_pointers_run) can DROP/CREATE INDEX under the runtime
-- role.
--
-- Applied OUT-OF-BAND as the database owner via `striatum daemon owner-ddl apply
-- --owner-url …`, BEFORE the daemon's runtime self-bootstrap runs the runtime
-- migrations (the deploy runbook ordering, docs/how-to/postgres-transition.md). So
-- after this bundle lands, the table is genuinely runtime-owned and migration 0039
-- succeeds under striatumd_rw.
--
-- WHY THIS BUNDLE IS REQUIRED (the owner-DDL trap). process_supervisor_pointers /
-- process_supervisors / daemon_supervisors are CREATEd by runtime migrations 0002
-- and 0005 — BELOW the floor-27 owner-DDL guard and PRE-DATING the RFC 0110
-- two-role split. On a two-role production deploy the schema migrations
-- sql/NNNN_*.sql are applied by the OWNER role through `daemon migrate-db
-- --admin-url …` (the runbook's fresh-bootstrap step), so these tables are owned by
-- the bootstrap/owner role (e.g. halbritt), NOT the runtime role striatumd_rw.
-- Verified live: on host `proximal` (striatum_daemon) process_supervisor_pointers,
-- process_supervisors, and daemon_supervisors are all owned by `halbritt` while
-- job_recovery_state (transferred by bundle 0018) is owned by striatumd_rw. A
-- DROP INDEX / CREATE INDEX on an owner-owned table requires ownership of the
-- underlying table, so a runtime migration (applied by the daemon's own
-- ConnectAndMigrate self-bootstrap as striatumd_rw) reshaping
-- idx_process_supervisor_pointers_run would fail `must be owner of table
-- process_supervisor_pointers` (SQLSTATE 42501) and crash-loop the daemon — exactly
-- the #441/#442 incident class. The migrations_test owner-DDL guard does NOT catch
-- this: its regex matches only ALTER/DROP TABLE, not DROP INDEX / CREATE INDEX, and
-- pgtest is single-role so it never reproduces the two-role ownership failure.
--
-- THE FIX. Transfer ownership of the supervisor-table cohort to striatumd_rw here,
-- as the owner, BEFORE the runtime migrations. After the transfer the runtime role
-- owns the tables and migration 0039's DROP/CREATE INDEX succeeds under
-- striatumd_rw. This is the SAME mechanism bundle 0018 used for the
-- job_recovery_state / barrier cohort.
--
-- THE COHORT. All three supervisor tables get transferred (RFC 0139 Q4: the
-- coalesce and the index drop apply uniformly across the siblings). They carry
-- runtime DATA only (supervisor liveness pointers); none is an authority-gated
-- owner surface, so moving ownership to the runtime role does NOT weaken any RFC
-- 0110 read/write boundary (those boundaries are the owner-owned SECURITY DEFINER
-- functions + the revokes in bundles 0003-0006, which this bundle does not touch).
--
-- IDEMPOTENT + DEPLOY-ORDER SAFE. ALTER TABLE … OWNER TO is a no-op when the table
-- is already owned by striatumd_rw, so a re-run — or applying onto a DB an operator
-- already hand-reassigned — is safe. It is guarded by an EXISTS-striatumd_rw probe
-- (a single-role deploy that never provisioned the runtime role is left untouched)
-- and a to_regclass probe per table (a deploy behind on the runtime migration that
-- created the table skips it). In pgtest (single-role) the per-test pool role both
-- owns the tables and is a member of the cluster striatumd_rw role, so the transfer
-- is a real ownership move to the shared parent role and is a no-op on re-run.
--
-- PREREQUISITES (both satisfied here, applied as the owner; same as bundle 0018):
--   1. The owner role MUST be a member of striatumd_rw (PostgreSQL requires the
--      role running ALTER … OWNER TO to be a member of the new owning role). The
--      two-role bootstrap grants this; pgtest grants it in ensureRuntimeRole.
--   2. striatumd_rw MUST hold CREATE on schema striatumd (PostgreSQL requires the
--      NEW owner of a table to be able to create objects in the table's schema).
--      Bundle 0018 already GRANTs this idempotently; we GRANT it again here so this
--      bundle is self-contained on a fresh deploy that applied bundles out of band.

DO $$
DECLARE
  v_table text;
  v_cohort text[] := ARRAY[
    'process_supervisor_pointers',    -- migration 0005; index reshaped by 0039
    'process_supervisors',            -- migration 0005
    'daemon_supervisors'              -- migration 0002
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
VALUES ('supervisor_pointer_runtime_ownership', false, 19)
ON CONFLICT (capability) DO NOTHING;
