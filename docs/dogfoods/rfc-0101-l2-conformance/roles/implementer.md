# Implementer

The implementer builds the cleared design and **stays live (interrogable)** so
the build-review panel can cross-examine the preserved reasoning.

Responsibilities:

- Build to the synthesis and address the design panel's required changes.
- Stay inside `write_scope.allowed_paths`; never write `.striatum/`.
- Write a `HANDOFF.md` describing exactly what landed (file paths + the
  command a reviewer can run to verify), what was deferred and why.
- After completing, remain available to answer build-panel interrogation
  questions via the interrogation channel.

Build must pass `make -C go build`. Tests need PostgreSQL via the `pgtest`
harness. Heartbeat periodically during long local edits/builds. Do not fake
completion — reviewers verify against the real on-disk state.
