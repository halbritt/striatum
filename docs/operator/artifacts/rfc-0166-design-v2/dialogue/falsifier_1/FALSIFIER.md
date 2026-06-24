# FALSIFIER 1 - RFC 0166 v2 C1 novelty-clock re-attack

author: falsifier-reviewer-003

## Claim Challenged

The v2 holder claims C1 is resolved because `novelSealedProgressAt` is the single novelty-aware primitive used by every reset surface: the Part-1 floor, the Part-4 telomere reset, and the RFC 0131 confidence-gate `progressAdvanced` (`docs/operator/artifacts/rfc-0166-design-v2/dialogue/holder/HOLDER.md:88-118`, `:232-297`, `:343-370`). I agree the direct undeclared-junk raw-clock hole is repaired on paper: the floor no longer reads raw `jobSealedProgressAt`, undeclared rows are excluded by declared `logical_name`, the telomere reset reads `novelSealedProgressAt`, and the confidence gate replaces raw `sealedAt` with `novelSealedProgressAt`.

C1 still does not clear, because the proposed timestamp is not actually equivalent to the required strict-increase cursor.

## Concrete Refutation

C1 requires the strict cursor from v1 Claim 3.1/3.3, hardened to declared/milestone artifacts, to drive every reset surface (`docs/operator/workflows/rfc-0166-design-v2/SEED.md:55-63`; `docs/operator/artifacts/rfc-0166-design/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md:77-88`). That cursor has three dimensions: distinct declared content hashes, sealed verdict count, and highest satisfied required-artifact milestone index (`docs/operator/artifacts/rfc-0166-design/dialogue/holder/HOLDER.md:167-174`). The v2 holder repeats that D/V/M position (`HOLDER.md:240-249`) and then claims `novelSealedProgressAt` strictly increases iff that position advances (`HOLDER.md:278-281`).

But the SQL shown for `novelSealedProgressAt` only timestamps two things: `max(first_seen_at)` over declared artifacts grouped by `content_sha256`, and `max(verdicts.created_at)` (`HOLDER.md:251-267`). There is no term for the time a required expected-artifact milestone is first satisfied. Grouping by `content_sha256` collapses later required milestones that intentionally or accidentally reuse bytes already seen in an earlier declared artifact.

Counterexample:

1. A job declares two required expected artifacts in order: `phase_1` at `docs/a.md`, then `phase_2` at `docs/b.md`.
2. At T1 it publishes `phase_1` with content hash H.
3. At T2 it publishes `phase_2` with the same content hash H.
4. This is a valid artifact shape: artifact uniqueness is scoped to `(repository_id, run_id, job_id, logical_name, attempt)` and `(repository_id, run_id, repo_path, content_sha256, attempt)`, so a different `logical_name` and different `repo_path` can reuse the same content hash (`go/pkg/db/sql/0018_artifact_attempt_scope.sql:26-36`; `go/pkg/mutations/artifact.go:217-299`).
5. The M dimension, highest satisfied required milestone index, advances at T2. Under the required strict cursor, novelty advanced at T2.
6. The v2 timestamp does not advance: the `GROUP BY content_sha256` bucket for H keeps `min(created_at)=T1`, so `max(first_seen_at)` remains T1 and there is no verdict term to rescue it.

Every reset surface then reads the wrong value. The Part-1 floor can age from T1 and breach even though the job satisfied a later required milestone at T2; the Part-4 telomere counter fails to reset on that genuine milestone; and the RFC 0131 confidence gate fails to set `progressAdvanced` for that milestone, so `consecutive_silent_sweeps` can continue climbing. That is not the old junk-row attack, but it is still a C1 failure: the reset surfaces do not consume the full strict-increase novelty cursor the constraint required.

## Strongest Rebuttal

The strongest defense is that duplicate bytes are not meaningful novelty, so only distinct declared content hashes plus verdicts should count. That would be a coherent smaller primitive, but it is not the primitive this v2 spec claims or the constraint required. The spec explicitly keeps M as an independent cursor dimension and says M is carried by the declared-artifact term (`HOLDER.md:278-281`). The SQL does not carry it.

If the build wants M to count, `novelSealedProgressAt` needs a timestamp for first satisfaction of each required expected-artifact milestone, or the persisted cursor update must stamp `last_novel_sealed_progress_at` when M advances. If the build does not want M to count, the spec must delete M from the primitive and re-justify the false-kill and telomere semantics for duplicate-content required artifacts.

## Unanswered Gap / Required Test Shape

Add a C1 falsification test with two required declared milestones where the second milestone publishes byte-identical content at a later time. Assert that the strict cursor and `last_novel_sealed_progress_at` advance to the second milestone time; the Part-1 floor moves; the Part-4 telomere reset fires; and the confidence gate treats `progressAdvanced` as true, including after a daemon restart. Without that test and a timestamp term for M, C1 is not genuinely discharged.
