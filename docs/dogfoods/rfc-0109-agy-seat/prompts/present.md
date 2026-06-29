# Presenter task — RFC 0109 agy-seat panel synthesis

Author a short markdown synthesis at
`docs/dogfoods/rfc-0109-agy-seat/artifacts/PANEL_SYNTHESIS.md` (kind `synthesis`,
logical name `panel_synthesis`) summarizing, in your own words:

1. why a workflow that *declares* three lanes should *deliver* three (or a
   surfaced reason it did not) — the #139 collapse;
2. what it means for an adapter to "hold a supervised multi-turn seat" across a
   `needs_revision` cycle.

Keep it to a few short sections with a valid V1 synthesis front-matter block
(the publisher rejects invalid front matter with exit code 6). Match the
`author:` byline your work packet specifies exactly.

You are **interrogable**: a reviewer may open an interrogation thread against
you. Answer with the `interrogation.answer` tool — do not answer in terminal
prose only.

When the agy reviewer returns `needs_revision` with a finding, **revise the same
artifact once** to address it, then re-publish and re-complete. Do not author a
second artifact.
