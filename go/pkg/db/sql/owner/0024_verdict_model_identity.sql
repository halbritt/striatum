-- P1-VERDICT-MODEL-IDENTITY: declared model identity on verdict rows.
--
-- striatumd.verdicts is owner-held in the two-role deployment, so these
-- additive columns live in an owner bundle rather than a runtime migration.
-- Writers populate them only from declared workflow/job metadata, or with an
-- explicit operator/recovery basis for override paths. Existing rows remain
-- NULL and read as unknown; this bundle performs no historical rewrite.

ALTER TABLE striatumd.verdicts
  ADD COLUMN IF NOT EXISTS model_identity_declared text;

ALTER TABLE striatumd.verdicts
  ADD COLUMN IF NOT EXISTS model_family_at_record text;

ALTER TABLE striatumd.verdicts
  ADD COLUMN IF NOT EXISTS model_identity_basis text;

ALTER TABLE striatumd.verdicts
  ADD COLUMN IF NOT EXISTS model_co_blindness_at_record text;
