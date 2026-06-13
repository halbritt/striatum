-- RFC 0123 — owner bundle 0007: artifact placement.
--
-- Placement is owner-table DDL on striatumd.artifacts, so it lives in an
-- owner/admin bundle rather than a regular runtime migration (D187). The new
-- 18-argument append_artifact_row overload accepts placement as the final
-- argument so the existing 17-argument function stays available for older
-- binaries during a rolling upgrade.

ALTER TABLE striatumd.artifacts
  ADD COLUMN IF NOT EXISTS placement text;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
      FROM pg_constraint
     WHERE conrelid = 'striatumd.artifacts'::regclass
       AND conname = 'artifacts_placement_check'
  ) THEN
    ALTER TABLE striatumd.artifacts
      ADD CONSTRAINT artifacts_placement_check
      CHECK (
        placement IS NULL OR placement IN (
          'blob_exhaust',
          'git_publication',
          'git_pointer_manifest'
        )
      );
  END IF;
END
$$;

CREATE OR REPLACE FUNCTION striatumd.append_artifact_row(
  p_repository_id text,
  p_artifact_id text,
  p_run_id text,
  p_job_id text,
  p_session_id text,
  p_logical_name text,
  p_artifact_kind text,
  p_repo_path text,
  p_content_sha256 text,
  p_size_bytes bigint,
  p_publish_mode text,
  p_created_at timestamptz,
  p_author_line text,
  p_blob_key text,
  p_blob_sha256 text,
  p_blob_content_type text,
  p_attempt integer,
  p_placement text
)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = striatumd, public, pg_temp
AS $$
BEGIN
  PERFORM striatumd.assert_daemon_authority();

  INSERT INTO striatumd.artifacts (
    repository_id, artifact_id, run_id, job_id, session_id, logical_name,
    artifact_kind, repo_path, content_sha256, size_bytes, publish_mode,
    created_at, author_line, blob_key, blob_sha256, blob_content_type, attempt,
    placement
  ) VALUES (
    p_repository_id, p_artifact_id, p_run_id, p_job_id, p_session_id, p_logical_name,
    p_artifact_kind, p_repo_path, p_content_sha256, p_size_bytes,
    COALESCE(p_publish_mode, 'create'),
    p_created_at, p_author_line, p_blob_key, p_blob_sha256, p_blob_content_type,
    COALESCE(p_attempt, 1), NULLIF(p_placement, '')
  );

  RETURN p_artifact_id;
END
$$;

CREATE OR REPLACE FUNCTION striatumd.append_artifact_row(
  p_repository_id text,
  p_artifact_id text,
  p_run_id text,
  p_job_id text,
  p_session_id text,
  p_logical_name text,
  p_artifact_kind text,
  p_repo_path text,
  p_content_sha256 text,
  p_size_bytes bigint,
  p_publish_mode text,
  p_created_at timestamptz,
  p_author_line text,
  p_blob_key text,
  p_blob_sha256 text,
  p_blob_content_type text,
  p_attempt integer
)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = striatumd, public, pg_temp
AS $$
BEGIN
  RETURN striatumd.append_artifact_row(
    p_repository_id, p_artifact_id, p_run_id, p_job_id, p_session_id,
    p_logical_name, p_artifact_kind, p_repo_path, p_content_sha256,
    p_size_bytes, p_publish_mode, p_created_at, p_author_line, p_blob_key,
    p_blob_sha256, p_blob_content_type, p_attempt, NULL
  );
END
$$;

REVOKE ALL ON FUNCTION striatumd.append_artifact_row(
  text, text, text, text, text, text, text, text, text, bigint, text,
  timestamptz, text, text, text, text, integer, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION striatumd.append_artifact_row(
  text, text, text, text, text, text, text, text, text, bigint, text,
  timestamptz, text, text, text, text, integer, text) TO striatumd_rw;

REVOKE ALL ON FUNCTION striatumd.append_artifact_row(
  text, text, text, text, text, text, text, text, text, bigint, text,
  timestamptz, text, text, text, text, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION striatumd.append_artifact_row(
  text, text, text, text, text, text, text, text, text, bigint, text,
  timestamptz, text, text, text, text, integer) TO striatumd_rw;
