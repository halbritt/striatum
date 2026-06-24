# FALSIFIER - RFC 0164 v2 C2 unknown-key negative gap

author: falsifier-reviewer-002

## Gate impact

Needs revision. The v2 SPEC does fix the original stable `[alias]` / `[pager]` wedge by introducing non-blocking `gate.read_gadget_observed` and by making the positive half of `false_positive_benign_test` assert no blocker, no `recovery.quarantine_lane` ref, and a second read without repo edit or human clear. That part is materially better than v1.

But C2 still is not genuinely discharged because the paired negative control and the unknown-key route are underspecified. A33 says to plant "an unknown/unattested key (a config key not in the §0 recognized taxonomy)" and assert it hard-refuses into `recovery.quarantine_lane`. Section §0 is a daemon git call-site and route taxonomy, not a config-key taxonomy. Taken literally, that rule either hard-refuses ordinary benign Git config that is not in §0, recreating the false-positive wedge through a different route, or it proves nothing about unknown executable gadgets.

## Claim challenged

The challenged claims are A24, A32, A33, and the assertion that the observed / blocked / refused model is coherent enough to keep false positives non-blocking while unknowns never silently pass.

The SPEC says:

- `gate.read_gadget_observed` is non-blocking for recognized keys whose execution is already neutralized, including benign `[alias] co=checkout` / `[pager] log=less -FRX`.
- `gate.read_gadget_refused` is a hard refusal into the human-cleared lane for an unknown/unattested key with no taxonomy entry and no green-corpus coverage.
- A33's negative half plants an unknown/unattested config key not in the §0 recognized taxonomy and expects `recovery.quarantine_lane` plus a failed read.

That is not enough to build or verify the property.

## Concrete failing case

Use a target repo whose local config contains stable, ordinary, non-executable config that the holder does not enumerate in §0, for example:

```ini
[color]
    ui = auto
[remote "origin"]
    url = https://example.invalid/repo.git
[branch "main"]
    merge = refs/heads/main
```

These keys are not in the v2 §0 taxonomy because §0 enumerates git spawn sites and route classes, not a safe/unsafe config-key language. They also have no green-corpus coverage in §5. If A32/A33 are implemented literally, the first allowlisted read sees keys with no taxonomy entry and no green-corpus coverage, emits `gate.read_gadget_refused`, creates a `recovery.quarantine_lane` ref, and blocks until a human clears or edits harmless config. That is the same liveness failure C2 was meant to eliminate, just moved from `[alias]` / `[pager]` to any unregistered benign config.

If the intended implementation is instead "ignore inert unknown config keys and refuse only unknown executable gadget carriers," then the SPEC does not define the classifier. The A33 negative fixture of "a config key not in the §0 recognized taxonomy" can be satisfied by a key Git ignores, so the test would be fake: hard-refusing it proves overblocking, while letting it pass proves nothing about a real future gadget. A genuine negative needs an explicit executable-but-unattested class, or a defined candidate-key extractor that distinguishes inert unknown config from unknown execution carriers.

## Additional state-model contradiction

The residual rows expose the same classifier hole. The SPEC says the `quarantine_addA_filter_clean` residual is expected-fail vs Layer 2 and "refused-not-executed in P0." It also says residual recognized keys are part of `gate.read_gadget_observed`, which pins no blocker, creates no recovery ref, and never blocks. Then §8.3 reserves `gate.read_gadget_refused` for unknown/unattested keys only.

A recognized-but-not-yet-neutralized residual such as `filter.<driver>.clean` on whole-tree `add -A` therefore has no coherent state: observed would be non-blocking, refused would violate the "unknown only" rule, and blocked would reintroduce machine-clearable liveness behavior. That does not directly re-open the benign `[alias]` / `[pager]` case, but it means the state machine still is not a single coherent blocker-vs-observability model.

## Strongest rebuttal and why it fails

The strongest rebuttal is that "unknown/unattested" obviously means an executable Git gadget family, not every harmless config key. That is the right intention, but it is not what the SPEC says. It points to §0 as the recognized taxonomy, and §0 is a route/call-site table. It does not define a config-key registry, a safe inert-key policy, or a detector language for executable-but-unattested keys.

A second rebuttal is that A33's positive half protects the important false-positive case. It protects only `[alias]` / `[pager]`. It will not catch an overbroad unknown-key refusal that wedges on common benign config, and it will not prove a true unknown executable carrier hard-refuses.

## Required fix before the gate can clear

Before C2 can clear, the SPEC needs to make the negative half of A33 real:

- Define the scanned config-key domain precisely, separating inert benign keys from execution-capable candidate keys.
- Define what makes a key "recognized," "covered by green corpus," and "unknown/unattested" in a config-key registry, not by reference to the call-site taxonomy.
- Make inert unknown benign keys non-blocking, or explicitly justify and test a narrower scanner that never sees them.
- Use an executable-but-unattested fixture for the negative case, and assert it hard-refuses into `recovery.quarantine_lane` without making ordinary benign config a human-cleared blocker.

## Carry-forward regression check

I did not find a separate carry-forward regression in the layered-severance posture, the `GIT_CONFIG_COUNT` omission reasoning (A7/A16), the `ErrGitEnvUnavailable` refuse-not-degrade floor (A8), the P0 no-truncated-graph / Slice-2 parity harness (A21/A21b), the canonicalization / no-attestation-before-exec / decay re-attest mechanics (A22/A23/A25), or the four §0 source corrections. The standing gate issue from this lens is the incomplete C2 classifier and fake/overbroad A33 negative control above.