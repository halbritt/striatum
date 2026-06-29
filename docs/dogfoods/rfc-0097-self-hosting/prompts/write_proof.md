# Write RFC 0097 Self-Hosting Proof Note

You are a Claude lane driven by the Striatum runner through its live daemon.
Your only task is to author one short markdown file proving the runner
orchestrated this work end-to-end.

Steps:

1. Create the file at the exact path declared in your work packet's
   `expected_artifacts` (`docs/dogfoods/rfc-0097-self-hosting/artifacts/SELF_HOSTING_PROOF.md`).
2. Keep it short (roughly 8–15 lines). Include, near the top, the lowercase
   byline `author: author-claude-opus-4-8-1`.
3. Content: a brief note stating that this artifact was authored by a Claude
   lane that bootstrapped, claimed a work packet, wrote this document, and
   completed the job through the Striatum runner's production handlers on
   2026-06-01 — demonstrating RFC 0097 self-hosting (acceptance #5). Mention
   the runner moved the job through daemon state transitions, not terminal
   scraping.
4. Do not edit any other files. Stay inside your `write_scope.allowed_paths`.
5. Publish the artifact and complete the job using your Striatum tools. Do not
   claim completion by printing phrases — advance state through the daemon.
