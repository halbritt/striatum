# FALSIFIER - RFC 0164 v2 C2 classifier gap

author: falsifier-reviewer-004

## Challenge

C2 should not clear on the current v2 SPEC. The revision fixes the original direct wedge for a stable benign `[alias]` / `[pager]`: `gate.read_gadget_observed` is explicitly non-blocking, pins no blocker, creates no `recovery.quarantine_lane` ref, and the positive half of `false_positive_benign_test` now checks a second read with no repo edit or human clear (`HOLDER.md:64-95`, `HOLDER.md:679-747`). That is real progress over v1.

The remaining gap is the negative half of C2. A32/A33 define the hard-refusal case as an unknown/unattested key with "no taxonomy entry, no green-corpus coverage," and A33 says to plant "a config key not in the §0 recognized taxonomy" (`HOLDER.md:703-747`). But §0 is not a config-key taxonomy. It is a daemon git call-site and route table: funnels, subcommands, route classes, current env, and P0 closure (`HOLDER.md:154-220`). It does not define the scanned config-key domain, which keys are inert and ignored, which keys are execution-capable candidates, or what makes a candidate "recognized" versus "unknown."

That makes the negative control either overbroad or fake:

- If implemented literally, any ordinary local config key absent from §0 and absent from the green corpus can hard-refuse. A repo with stable harmless config such as `color.ui=auto`, `branch.main.merge=refs/heads/main`, `remote.origin.url=https://example.invalid/repo.git`, or `core.abbrev=12` has keys that are not in the route taxonomy and are not §5 green-corpus gadget rows. Refusing those keys creates a human-cleared `recovery.quarantine_lane` path for benign retained config, which recreates the false-positive wedge through a different key family.
- If the intended implementation is "only execution-capable unknown keys refuse," the SPEC does not define the classifier or extractor that separates inert unknown config from unknown execution carriers. Then A33 can be satisfied by hard-refusing an inert key, which proves overblocking, or by letting inert unknowns pass, which proves nothing about a true future executable gadget.

## State-model contradiction

The residual rows expose the same classifier hole. The SPEC says arbitrary in-repo render and whole-tree `add -A` residuals are refused in P0 and sent to the human-cleared lane (`HOLDER.md:258-267`, `HOLDER.md:586-592`). But §8.3 defines `observed` as non-blocking for recognized keys, including residuals "slated for the minted-config omission and meanwhile refused," then defines `refused` only for unknown/unattested keys (`HOLDER.md:685-706`).

A recognized but not-yet-neutralized residual such as `filter.<driver>.clean` on whole-tree `add -A` therefore has no coherent state. `observed` cannot be right because it must never block; `refused` violates the "unknown only" rule; `blocked` is reserved for env/unwired defects and would bring back machine-clearable liveness semantics. This is not the original alias/pager wedge, but it shows the observed/blocked/refused model still lacks the config-key classifier needed to keep false positives non-blocking while unknown executable gadgets hard-refuse.

## Strongest rebuttal

The strongest rebuttal is that "unknown/unattested" obviously means an unknown executable Git gadget family, not every harmless config key. I agree that is the right intent. The problem is that the v2 SPEC does not say it. It points A33 at §0, and §0 is a route taxonomy, not a key registry. The build implementer has no falsifiable contract for which keys are scanned, which unrecognized inert keys are ignored, and which executable-but-unattested keys must refuse.

A second rebuttal is that the positive half of `false_positive_benign_test` covers the important benign case. It only covers `[alias]` and `[pager]`. It will not catch a broad unknown-key refusal that quarantines common benign config, and it does not prove a real unknown executable carrier cannot silently pass.

## Required fix before C2 can clear

Make the negative half of A33 precise enough to implement and falsify:

- Define a config-key classifier separate from the §0 route taxonomy: inert keys, recognized execution-capable keys, recognized residual keys, and unknown execution-capable candidates.
- State what happens to inert unrecognized benign keys; they must not create a job/run blocker or `recovery.quarantine_lane` ref.
- Use a negative fixture that represents an execution-capable but unattested key, for example by withholding a known executable family from a test-only registry or by defining a detector fixture that simulates a future executable carrier. The negative must prove hard-refusal for that class, not for arbitrary unknown config text.
- Align residual recognized keys with exactly one state: non-blocking observed only after neutralization, typed refusal while unneutralized, or blocked as a defect. Do not let one residual be both "observed" and "refused" by definition.

## Carry-forward regression check

I did not find a separate carry-forward regression in the layered-severance posture, `GIT_CONFIG_COUNT` omission reasoning (A7/A16), the `ErrGitEnvUnavailable` refuse-not-degrade floor (A8), P0 no-truncated-graph plus the Slice-2 parity gate (A21/A21b), evidence mechanics (A22/A23/A25), or the four §0 source corrections. From this C2 lens, the standing issue is the incomplete classifier and overbroad/fake A33 negative control above.
