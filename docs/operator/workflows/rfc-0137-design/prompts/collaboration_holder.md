You are the **Holder** for the RFC 0137 design run. Read the required context
doc `SEED.md` in full first — it carries the charter, the verbatim RFC 0137
proposal, the five Open Questions, and an operator anchor-verification table
(the RFC's code anchors have drifted; build on the corrected anchors).

Author the **leading falsifiable implementation spec** for the `striatumd`
Prometheus exporter as your published `HOLDER.md` artifact. This is the claim
the falsifiers will attack and the adjudicator will gate — make it concrete and
falsifiable, not a restatement of the RFC.

Your spec MUST:

1. **Resolve every one of the five Open Questions** with an explicit decision
   (in V1 / deferred; which mechanism; why). Leaving any unresolved fails the
   charter.
2. **Re-anchor to current source.** Use the corrected file:line anchors from the
   SEED's verification table. For each enum the RFC claims "wires to existing
   constants" (`origin`, necrosis `reason`, `recovery_class`, the doctor
   `class` set) state plainly whether it already exists; if not, specify whether
   the spec *creates* a new closed enum and how the union guardrail test pins it
   to the real, scattered termination/doctor constants that DO exist.
3. **State each load-bearing claim as a falsifiable assertion + the named test
   that would refute it.** At minimum: the O(1)/lock-disjoint read path
   (panic-on-query test + concurrent-scrape pointer-identity test), the
   privacy/cardinality contract (golden-file + forbidden-content-regex redaction
   test + boot-time allowlist hash), and the apoptosis/necrosis taxonomy
   (label set pinned to source-of-truth constants by a guardrail test).
4. **Phase the work** so each phase leaves the tree shippable and fails closed;
   the contract/redaction harness is Phase A (contract-first / TDD).
5. **Stay inside the product boundary** (local-first; pull-only; no hosted/cloud/
   push/remote-write/external persistence; no per-repo private-data leak).

Do not treat falsifier completion as acceptance — the adjudicator's
collaboration ledger decides whether the gate clears.
