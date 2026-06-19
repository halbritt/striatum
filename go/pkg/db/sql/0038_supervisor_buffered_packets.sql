-- #456 (FMA-006) — durable buffer for no-reader supervised_push packets.
--
-- supervise.send writes a work packet to a lane's stdin FIFO. When no reader is
-- attached (the agent process has not yet opened stdin, or was momentarily
-- between attaches) the O_WRONLY|O_NONBLOCK open returns ENXIO, so the daemon
-- BUFFERS the payload in a process-global in-memory map (pipeBuffers) and emits a
-- degraded supervisor.packet_buffered event instead of supervisor.packet_delivered
-- (#358). The in-memory buffer was NOT durable: a daemon restart dropped the map,
-- and there was no replay path. Self-driving (pull) lanes re-fetch the packet via
-- work.await_packet and self-heal, but a legacy supervised_push lane silently
-- missed the dropped packet and the run could wedge waiting on it.
--
-- This table makes a buffered push packet durable and replayable across a daemon
-- restart. When writeSupervisorPayload buffers a payload it also persists the row
-- here, keyed by (repository_id, supervisor_id, pipe_path) plus a monotone seq so
-- ordering is preserved. Before every delivery attempt the daemon hydrates the
-- in-memory pipeBuffers for that pipe from this table, so a post-restart
-- supervise.send (or any subsequent delivery) re-queues the persisted packets and
-- the existing reader-attach flush in writeToPipe replays them to the FIFO the
-- moment a reader is present. Once a reader drains the queue (a non-buffered
-- write), the persisted rows for that pipe are cleared. The buffer is bounded to
-- the same depth as the in-memory queue (10) — see writeSupervisorPayload, which
-- refuses to persist past the cap rather than accumulating unbounded.
--
-- Runtime migration, NOT an owner bundle: this is a NEW table created and DML'd
-- exclusively by the runtime role (striatumd_rw). It carries NO foreign key into
-- any owner-held table (repository_id / supervisor_id are bare columns; the
-- supervisor lifecycle owns its own teardown), per D215's runtime-vs-owner split,
-- so the runtime role can both create and write it and no later runtime migration
-- ALTERs a bootstrap-owned table (the #442 trap). A deployment behind on this
-- migration falls back to today's in-memory-only buffer (degrade-safe: the persist
-- / hydrate / clear helpers tolerate an absent table and behave as the
-- pre-#456 code path). Owner-applied at deploy + substrate_version bumped to 38.

CREATE TABLE IF NOT EXISTS striatumd.supervisor_buffered_packets (
  repository_id text   NOT NULL,
  supervisor_id text   NOT NULL,
  pipe_path     text   NOT NULL,
  seq           bigint NOT NULL,
  payload       bytea  NOT NULL,
  buffered_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (repository_id, supervisor_id, pipe_path, seq)
);

-- The daemon connects as striatumd_rw; grant it DML on the new table (an
-- owner-applied table would otherwise be inaccessible to the runtime role and the
-- delivery path would fail permission-denied). Matches the grant pattern every
-- other table-creating runtime migration uses (e.g. 0011 escalation_inbox, 0020
-- job_recovery_state).
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'striatumd_rw') THEN
    GRANT SELECT, INSERT, UPDATE, DELETE ON striatumd.supervisor_buffered_packets TO striatumd_rw;
  END IF;
END;
$$;
