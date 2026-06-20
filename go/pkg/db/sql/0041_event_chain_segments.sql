-- RFC 0136 P1 (D242) — chain-segment sealing for the per-repository event chain.
--
-- P0 (D241) pinned the policy: weekly partition granularity; `events` 3-month
-- retention, `audit_log` infinite; Q5 = generalize the `audit_segments`
-- "seal + boundary-hash + retention_state" model to the event chain. This
-- migration lands that generalization for `events` so an event partition can be
-- SEALED and proven-continuous BEFORE any partition DROP (P2/P4). Retention gets
-- a chain-safe boundary from day one — without this ledger, dropping a partition
-- is indistinguishable from tampering (RFC 0136 §"Preserving the hash-chain
-- contracts across the partition boundary").
--
-- This is a RUNTIME table per D215 (the per-object schema-ownership split): unlike
-- the owner-held `events` / `audit_log` / `audit_segments` tables, this ledger is
-- striatumd_rw-owned and carries NO foreign key into the owner-held `events`
-- table (an inbound FK into events is exactly the D215 / RFC 0136 §"chain-head FK
-- is the sharp edge" trap, made worse once the events PK reshapes to
-- (repository_id, event_id, created_at) in P2). Referential integrity between a
-- segment's boundary event ids/hashes and the actual `events` rows is enforced in
-- Go, inside the daemon's sealing path (pkg/mutations EventChainSegments), not by
-- a SQL FK — mirroring how `repo_event_chain_heads.last_event_id` integrity moves
-- to Go in P2.
--
-- The shape mirrors striatumd.audit_segments (0001_baseline.sql:68) one-for-one,
-- KEYED per repository_id because the event chain is per-repository (the audit
-- chain is a single global chain, so audit_segments needs no repository scope):
--   segment_id        — surfaced per-repo monotonic identity
--   first/last_event_id, first/last_created_at — the sealed event boundaries
--                       (created_at carried so the seam aligns to the time-range
--                        partition boundary in P2 without a join back to events)
--   first/last_hash   — the sealed boundary row hashes (the chain witnesses)
--   previous_segment_id / previous_segment_last_hash /
--   next_segment_first_previous_hash — the cross-segment hash witnesses that let a
--                       verifier prove "the chain was linear up to the seam, the
--                       seam hash is X, the chain resumed at hash X" WITHOUT the
--                       dropped rows.
--   retention_state   — 'active' until a future P4 retention executor flips it.
--   state             — open|sealed|purged (audit_segments uses open|closed|purged;
--                       'sealed' names the proven-continuous-and-frozen state the
--                       event chain seals into, which P4 then 'purged').
--
-- Forward-only. Append-only enforcement: an open segment is mutable (sealing
-- writes its boundaries once), a sealed/purged segment is frozen, and rows are
-- never deleted — the exact contract audit_segments enforces via
-- refuse_closed_segment_change / audit_segments_no_delete.

CREATE TABLE IF NOT EXISTS striatumd.event_chain_segments (
  repository_id text NOT NULL REFERENCES striatumd.repositories(repository_id),
  segment_id bigint NOT NULL,
  opened_at timestamptz NOT NULL DEFAULT now(),
  sealed_at timestamptz,
  first_event_id bigint,
  last_event_id bigint,
  first_created_at timestamptz,
  last_created_at timestamptz,
  first_hash text,
  last_hash text,
  previous_segment_id bigint,
  previous_segment_last_hash text,
  next_segment_first_previous_hash text,
  retention_state text NOT NULL DEFAULT 'active',
  state text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'sealed', 'purged')),
  PRIMARY KEY (repository_id, segment_id)
);

-- One open segment per repository at a time (the sealing path closes the open one
-- and opens its successor). A partial unique index, not a CHECK, because the
-- invariant is across rows of one repository, not within a row.
CREATE UNIQUE INDEX IF NOT EXISTS uq_event_chain_segments_one_open_per_repo
  ON striatumd.event_chain_segments(repository_id)
  WHERE state = 'open';

-- Fast "the sealed segment whose [first,last] event range contains event_id E"
-- and "the latest sealed segment for a repo" reads the verifier/retention path
-- needs.
CREATE INDEX IF NOT EXISTS idx_event_chain_segments_repo_range
  ON striatumd.event_chain_segments(repository_id, first_event_id, last_event_id);

-- Append-only invariants, mirroring audit_segments. A SEALED or PURGED segment is
-- frozen (only an open segment may be UPDATEd — that is the sealing write); no row
-- is ever DELETEd. Integrity of the boundary event ids/hashes against the real
-- events rows is enforced in Go, not here (no FK into owner-held events).
CREATE OR REPLACE FUNCTION striatumd.refuse_sealed_event_segment_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF OLD.state <> 'open' THEN
    RAISE EXCEPTION 'sealed daemon event chain segments are append-only';
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS event_chain_segments_sealed_no_update ON striatumd.event_chain_segments;
CREATE TRIGGER event_chain_segments_sealed_no_update
BEFORE UPDATE ON striatumd.event_chain_segments
FOR EACH ROW EXECUTE FUNCTION striatumd.refuse_sealed_event_segment_change();

CREATE OR REPLACE FUNCTION striatumd.refuse_event_segment_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'daemon event chain segments are append-only';
END;
$$;

DROP TRIGGER IF EXISTS event_chain_segments_no_delete ON striatumd.event_chain_segments;
CREATE TRIGGER event_chain_segments_no_delete
BEFORE DELETE ON striatumd.event_chain_segments
FOR EACH ROW EXECUTE FUNCTION striatumd.refuse_event_segment_delete();

-- Explicit GRANT to the runtime role, mirroring how repo_event_chain_heads
-- (0006_events_chain_anchors.sql) grants its runtime DML surface. The sealing
-- path INSERTs the successor open segment and UPDATEs the open segment's
-- boundaries; DELETE is denied (append-only, enforced both by the trigger and the
-- absence of the grant). A repository removal cascades segment rows via the
-- repository foreign key, never a standalone DELETE.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'striatumd_rw') THEN
    GRANT SELECT, INSERT, UPDATE ON striatumd.event_chain_segments TO striatumd_rw;
    REVOKE DELETE ON striatumd.event_chain_segments FROM striatumd_rw;
  END IF;
END;
$$;
