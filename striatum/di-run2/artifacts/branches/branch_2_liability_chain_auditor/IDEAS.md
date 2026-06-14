author: diverger-gemini-001

# Vantage Frame: liability_chain_auditor

Trace every decision to its last accountable owner; at which handoff does responsibility evaporate, and who is named when it fails?

## 1. Lease Ownership Attestation (Lane Death)
When a lane dies, the responsibility for progress evaporates. To prevent anonymous stalls, the fixture asserts that the daemon logs the exact lease UUID, process ID, and supervisor ID for every state transition. When a timeout occurs, the escalation payload must name the last active leaseholder rather than reporting a generic timeout.

## 2. Cryptographic Mock Signing (Non-deterministic Outputs)
To isolate non-deterministic text from behavioral assertions, mock agents in the CI fixture must cryptographically sign a metadata packet (hashes of write-scopes, author lines) for every artifact published. If an assertion fails, the CI test names the specific mock signature identity that produced the invalid state, pinning responsibility on the mock configuration.

## 3. Reviewer Replacement Chain of Custody (Reviewer Replacement)
When a reviewer is replaced mid-job, responsibility for progress is muddied. The daemon must maintain a sequential chain of reviewer IDs for the job. If the job fails to complete or escalates, the system names the entire chain of custody with precise timestamps, showing exactly who held the lease and for how long.

## 4. Schema Handoff Guard (Structural Assertions)
To distinguish between a writer producing bad content and a reader parsing it incorrectly, the runner's validation gate acts as the explicit liability transfer point. If the convergence critic fails to parse an ideation artifact, the test asserts that the runner must name either the writer (for schema violations) or the critic (for failing to parse valid schema).

## 5. Supervisor Loudness Budget (Silent Wedges)
To prevent silent wedges where responsibility evaporates into unbounded retry loops, the supervisor is held liable for progress. The fixture asserts that if any lane exceeds its heartbeat/lease budget without a completion or an escalation, the supervisor must log a "failure-to-report" escalation to PostgreSQL before termination.

## 6. Transport Proxy Digest (Transport Churn)
During network churn, responsibility evaporates in the gap between client submission and daemon receipt. The fixture runs all MCP traffic through an interceptor proxy. If a completion fails, the test asserts against the proxy's digest, pinning liability on either the client transport or the daemon socket handler as the loss point.
