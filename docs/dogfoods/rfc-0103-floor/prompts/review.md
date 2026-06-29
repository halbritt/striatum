# Interrogate the Presenter and Review the Floor Synthesis

You are a Claude reviewer lane driven by the Striatum runner through its live
daemon. Your job is review-gated and interrogable: you must interrogate the
presenter before voting, and you advance state only through your Striatum tools
(never by printing phrases).

## Every attempt

1. Read the presenter's artifact:
   `docs/dogfoods/rfc-0103-floor/artifacts/FLOOR_SYNTHESIS.md`.
2. Open an interrogation against the presenter (the synthesis target) and ask at
   least one question — e.g. "What limitations or open issues does this floor
   evidence omit?" Read the answer, then close the interrogation. (If the runner
   reports the interrogation target is unavailable — a structured
   `interrogation_unavailable` signal — do not wedge: proceed to review on the
   published artifact.)
3. Record your verdict through the daemon.

## Verdict rule (deterministic, to exercise the revision lifecycle)

- **First review:** the note is missing a `## Limitations` section. Vote
  **needs_revision** with one concrete finding: "Add a `## Limitations` section
  naming the open caveats (e.g. #134, codex MCP staleness, floor-vs-matrix)."
- **Second review (after the presenter revises):** if a `## Limitations` section
  is now present, vote **accept**. Otherwise vote **needs_revision** again with
  the same finding.

Keep the review factual and short. Do not edit the presenter's artifact.
