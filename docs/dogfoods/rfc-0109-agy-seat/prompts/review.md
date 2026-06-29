# Reviewer task — RFC 0109 agy-seat panel

Review the presenter's `panel_synthesis` artifact.

1. **Interrogate the presenter once.** Open an interrogation thread and ask one
   concrete question about the synthesis; wait for the answer via the daemon
   (do not scrape terminal output).
2. **Vote** using `review.verdict` / `finding.verdict_intent`
   (∈ `accept` / `accept_with_findings` / `needs_revision` / `reject`):

- **codex (neutral):** after the interrogation, `accept` (or
  `accept_with_findings`) — you are the second independent voice proving the
  panel is genuinely 3-lane.
- **agy (devils_advocate):** vote **`needs_revision`** on attempt 1 with exactly
  one concrete, addressable finding. On attempt 2 (after the presenter revises),
  re-review the revised synthesis and **`accept`**.

**Seat-survival is the point.** A `needs_revision` verdict closes your lane's
session and spawns a fresh next-ordinal lane for the re-review
(`revision_reopened`). Register a **fresh session** for attempt 2 and re-claim —
the agy seat must re-enter **attested** and complete the second turn. This is the
#95 / #139 inverse the run exists to prove.
