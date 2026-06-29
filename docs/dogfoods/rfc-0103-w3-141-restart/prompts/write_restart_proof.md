# Write RFC 0103 W3 #141 Daemon-Restart Survival Note

You are a Claude lane driven by the Striatum runner through its live daemon.
Your only task is to author one short markdown file proving the runner
orchestrated this work end-to-end across a mid-run daemon restart.

Steps:

1. Create the file at the exact path declared in your work packet's
   `expected_artifacts`
   (`docs/dogfoods/rfc-0103-w3-141-restart/artifacts/RESTART_SURVIVAL_PROOF.md`).
2. Keep it short (roughly 8–15 lines). Include, near the top, the lowercase
   byline `author: author-claude-opus-4-8-1`.
3. Content: a brief note stating that this artifact was authored by a Claude
   lane that bootstrapped, claimed a work packet, and completed the job through
   the Striatum runner's production handlers on 2026-06-02 — corroborating RFC
   0103 W3 (#141): the supervised lane survives a `systemctl restart striatumd`
   mid-run because the unit now uses `KillMode=process`. Mention the runner moved
   the job through daemon state transitions, not terminal scraping.
4. Do not edit any other files. Stay inside your `write_scope.allowed_paths`.
5. Publish the artifact and complete the job using your Striatum tools. Do not
   claim completion by printing phrases — advance state through the daemon.
