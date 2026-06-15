---
schema_version: "striatum.synthesis.v1"
artifact_kind: "synthesis"
inputs: ["striatum/di-run2/artifacts/CONVERGENCE.md"]
---

# Deepened Pick: Exception-Lane Cross-Dock

author: deepener-gemini-001

## How It Works

Under this system, the graduation fixture / CI runner purposefully injects lane-level faults—specifically, abrupt lane process termination ("lane death"), MCP transport interruptions ("transport churn"), and unexpected reviewer swaps ("reviewer replacement")—using a deterministic, seeded fault schedule. The daemon monitors these injected faults and maps them to active jobs, ensuring each exception is either gracefully resolved (e.g. by reissuing the lease to a new/replacement lane or reviewer session) or handled via a structured escalation path. Every active workflow packet has a defined lease duration and a total budget. If a fault prevents completion, the system must trigger a named, structured escalation containing the full context of the failed lease and session before the budget closes. Crucially, the test verifies that the system does not hang or drop silently into a stale state, asserting that every injected exception successfully transitions either to a successful terminal state (a complete job with valid artifact receipts) or a loud, recorded escalation. This architecture formalizes resilience checks by forcing all fault conditions to resolve through explicit, observable state transitions in the PostgreSQL database rather than ambient file modifications.

## Load-Bearing Risk

The principal load-bearing risk is that the recovery mechanism (re-leasing the job or routing to a replacement lane) itself hangs or gets stuck in a deadlock due to concurrency races or stale lease cleanups. If the timeout/budget for escalation is not perfectly aligned with lease expiry times, a deadlocked recovery loop might silently exceed the overall execution window without firing the loud escalation.

## First Concrete Step

Define a deterministic exception-injection framework in the test suite (such as a test helper in [runner.go](file:///home/halbritt/git/striatum/go/pkg/adapterconformance/runner.go)) that can intercept MCP requests and simulate process crashes or TCP connection loss at specified points in the agent lifecycle.

## Child Ideas

1. **Unilateral Lease Expiry Escalator**: A background sweeper in the daemon that automatically converts any stale lease exceeding its heartbeat deadline directly into a structured escalation without waiting for a client to request it.
2. **Reviewer Swap Contradiction Test (Reviewer Churn)**: A variation where the replacement reviewer deliberately issues a conflicting review verdict (e.g. Reject vs Approve) to verify that the runner handles contradictory feedback gracefully and terminates without deadlock.
3. **Heartbeat Churn Recovery**: A hybrid mechanism where MCP transport churn is injected by dropping 50% of heartbeat requests, testing if the client can successfully retry heartbeat calls over ephemeral network drops without invalidating the lease.
4. **Adversarial Resign-and-Reissue**: A test script where a reviewer lane abruptly resigns, leaving the daemon to dynamically reissue the review job to a distinct model family to certify cross-model handoffs under load.
