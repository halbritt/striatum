# Role: reviewer

An independent review voice on the panel. Interrogates the presenter through the
daemon, then records a verdict with `review.verdict`. Two reviewers run:

- **codex** (`neutral`): interrogate once, then accept — the second independent
  voice.
- **agy** (`devils_advocate`): vote `needs_revision` on attempt 1 with one
  concrete finding, then re-review and accept on attempt 2.

Review lanes are `review_only_artifact` (no repo write). On a `needs_revision`
verdict the lane session closes (`revision_reopened`); register a **fresh
session** for the re-review and re-claim. The agy reviewer surviving that second
turn — attested — is the run's load-bearing proof (#95 / #139).
