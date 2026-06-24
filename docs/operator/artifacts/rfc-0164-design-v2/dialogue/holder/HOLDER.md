# HOLDER — RFC 0164 P0 falsifiable implementation SPEC, v2 (read-side + funnel git neutralization, the complete git-surface taxonomy, and the three-state gate-evidence recovery contract)

author: holder-author-001

> This is the **v2 revision** of the RFC 0164 P0 SPEC. v1 was source-anchored and
> its posture/omission reasoning was correct, but its single adjudication cycle
> returned **`needs_revision`** on two standing, gate-critical, unrebutted
> challenges. This revision **discharges both** and **carries forward, unregressed,
> everything v1 cleared**. It is the published claim the two falsifiers re-attack.
> I re-verified every named call site against current `go/` source (greps and
> `file:line` reads recorded in §0); the taxonomy below is grep-backed, not trusted
> prose. Scope is **P0 only**; §1 fixes the boundary precisely and §9 names the
> Slice 2 / Slice 3 seams P0 leaves.

---

## Addressing the cycle-1 constraints (the auditable revision map)

This is the first thing the adjudicator and falsifiers should check: **what changed,
and what did not.**

### C1 — severance is NOT complete → **RESOLVED** (§0 taxonomy, §2–§5, §11)

The v1 "exhaustive" §0 C-2 surface enumerated only the **read funnel**
(`localGit.output`, `readGitOutput`) and direct read execs. It **omitted the entire
daemon mutation/integration funnel** — `defaultRunGitWorktreeCommand`
(`mutations/worktree.go:1603`), `integrateGit` (`mutations/integrate.go:194`, which
just wraps the worktree runner), and `runGitWithEnv`
(`mutations/recovery_quarantine_lane.go:440`) — which run **gadget-bearing git under
the daemon identity** against attacker-controlled repos/worktrees, including a
**live `recovery_quarantine_lane.go:425` `status`→`core.fsmonitor` RCE** the v1 SPEC
implied was closed but is not. Resolution:

1. **§0 replaces the C-2 site-list with a COMPLETE git-surface TAXONOMY** — every
   funnel and every direct exec across `reads`/`mutations`/`verifier`/`agentloop`,
   each classified by **route class** (R read-only · S status/fsmonitor · C
   commit/hooks · M index-mutation/filter · D diff-or-render/textconv · W
   worktree-admin · L lane-side · X CLI-only) and by **current `cmd.Env`** (nil /
   `os.Environ()`-sourced / closed). It is the test's allowlist (A2), not prose.
2. **Every daemon-run route is routed through the closed env** (`gitEnv()` for reads,
   `mutationEnv()` for index/commit) — which also **closes the two further
   `os.Environ()`-sourced false-negatives** v1's C-4 found only at `receipt.go:605`:
   `recovery_quarantine_lane.go:378` and `barrier_fanin.go:877` build
   `append(os.Environ(), …)` envs (A31).
3. **Every in-repo-config-sensitive route is closed in P0 by a concrete mechanism**,
   not left as a silent residual: `status`→fsmonitor by the demoted fixed-key `-c
   core.fsmonitor=` interim (A28); `commit`→hooksPath by `-c core.hooksPath=<empty>`
   on the porter commit (A30); the porter `add`→filter.clean by **filter-free
   staging plumbing** (`hash-object --no-filters` + `update-index --cacheinfo`, A29).
   The arbitrary-driver omission closure (minted config) remains the **named** Slice-2
   form, but the three gate-named rows do **not** depend on it.
4. **§4/A12 is reconciled HONESTLY and the §11 green-build claim is made TRUE.** The
   invariant **inspects helper call sites** — there is in-tree precedent: the existing
   guard `git_invocation_guard_test.go:49-53` already switches on the helper names
   `runGitWorktreeCommand`/`integrateGit` and reads `call.Args[2]` as the subcommand.
   The v2 invariant extends exactly that machinery to ban any funnel/helper or direct
   git exec whose env is `nil` or `os.Environ()`-sourced. Because **every** funnel/
   helper exec is routed through `safegit` in P0, the invariant is **green in P0** —
   A2 and A12 are TRUE, not retracted.
5. **`recovery_quarantine_lane.go:425` is explicitly closed** (§2.2, A28) and the
   three corpus rows `quarantine_status_fsmonitor`, `porter_add_filter_clean`,
   `porter_commit_hookspath` are added **red-before / green-after** (§5, A28–A30).

### C2 — a false-positive WEDGES legitimate work → **RESOLVED** (§8.3)

v1 made a recognized-and-neutralized gadget a **machine-clearable blocker** whose
only clear condition was fingerprint decay — which never fires on stable benign
config, so a benign-but-retained `[alias]`/`[pager]` wedged until a human edited
benign config; and the contract self-contradicted (it called the record a `blocker`
while claiming observability-only). Resolution — **one coherent three-state model**
(SEED option (a)), split by clearer-of-record:

- **`gate.read_gadget_observed`** — **NON-BLOCKING telemetry** for any **recognized**
  key whose exec is **already neutralized by construction** (Layers 1+2: omitted from
  the closed env, run under the demoted fixed-`-c`, staged filter-free, or — Slice 2 —
  minted away). A benign `[alias] co=checkout` / `[pager] log=less -FRX` lands here.
  It pins **no** blocker, creates **no** recovery ref, and never blocks a job/run.
  Crucially this does **not** require proving an arbitrary alias value benign (the
  falsifier showed that is unsound): a recognized key — benign **or** hostile — is
  **already inert**, so it is observed, never blocking.
- **`gate.read_gadget_blocked`** — the **reserved blocking** state with the A23
  decay-TOCTOU semantics, fired **only** for a condition that **genuinely could not be
  neutralized at preflight** (e.g. the closed env could not be built →
  `ErrGitEnvUnavailable`, or a config-sensitive mutation route reaching exec before
  its P0 closure is wired). Machine-clearable by decay; exec re-attests (A23 intact).
- **`gate.read_gadget_refused`** — **hard refusal into the existing human-cleared
  `recovery.quarantine_lane`** for an **unknown/unattested** key (no taxonomy entry,
  no green-corpus coverage). No silent unknown-pass (A24/A32).

`false_positive_benign_test` is made **load-bearing** (§8.3, A33): plant the benign
config, run an allowlisted read, assert **no** job/run blocker, **no**
`recovery.quarantine_lane` ref, a `gate.read_gadget_observed` (non-blocking) event at
most, and that a **second** read proceeds with no repo edit and no human clear; the
paired negative plants an unknown key and asserts it **hard-refuses** into the
human-cleared lane.

### Carry-forwards — INTACT, not reopened, not regressed

| Carried from v1 (CLEARED) | Where, unchanged | Assertions |
|---|---|---|
| Layered-severance posture (denylist demoted to telemetry) | §1, §6, §8 | — |
| Omission IS neutralization (`GIT_CONFIG_COUNT` beats `-c`) | §3 | A7 (test **strengthened**, §3), A16 |
| Env floor REFUSES, never degrades (typed `ErrGitEnvUnavailable`) | §3.2 | A8 (now also `mutationEnv`) |
| Slice-0 no-truncated-graph + alternates/refs/benign-key parity harness as the binding Slice-2 gate | §7 | A21, A21b |
| Evidence mechanics: canonicalization golden vectors (A22), no-attestation-before-exec doctor barrier (A25), decay-TOCTOU re-attest (A23) | §8.1–§8.2 | A22, A23, A25 |
| The four §0 source corrections C-1..C-4 | §0 | — (now extended, not re-found) |

Nothing above is re-litigated. The only changes to the carry-forwards are
**additive hardening**: A7's test becomes a direct structural assertion (per
Falsifier 1's secondary note that a sentinel can go green for the wrong reason), A8
extends to `mutationEnv`, and the attestation/barrier surface (A25) now enumerates
the funnel exec sites it previously missed.

---

## Coverage map — how this SPEC discharges Goals G1–G4

| Goal | What it demands | Where discharged | Load-bearing assertions |
|------|-----------------|------------------|--------------------------|
| **G1** | No untrusted config/env value reaches command execution under the daemon identity through *any* git invocation — structurally, not by a forgettable checklist | §0 (complete taxonomy), §2 (chokepoint over reads **and** funnel), §3 (`gitEnv`/`mutationEnv` omission), §4 (compile-time invariant over helper call sites) | A1–A4, A6–A14, **A27–A31** |
| **G2** | Auditable: live policy version + closed-env proof recoverable; a gate that silently passes an *unknown* gadget is faked | §8 (golden vectors, attestation over the full exec surface, decay-TOCTOU) | A22–A25, **A32** |
| **G3** | A tripped repo recovers without a code change; a false-positive must not wedge a benign repo | §8.3 (**three-state** recovery), §7 (P0 answer-equivalence) | A20, A24, **A26, A33** |
| **G4** | Regression-tested by a planted-attack corpus that executes under the old path and no-ops under the new one | §5 (red-team corpus incl. the **three funnel rows**) | A15–A19, **A28–A30** |

The full assertion ledger is **§10** (A0–A33). The P0 boundary + Slice 2/3 seams are
**§9**. The build manifest is **§11**.

---

## §0 — Verified source baseline and the COMPLETE git-surface taxonomy

The holder verifies, does not trust. I re-read every named git site on current `go/`
source. **Verified true and carried from v1 (unchanged):**

- `reads/git_snapshot.go:193` `localGit.output` runs `status`/`rev-parse`/`log` with
  **no `cmd.Env`** (exec at `:200`); it enforces a small allowlist + arg denylist
  (`validateGitArgs`, `:226-244`). **Confirmed.**
- `mutations/git_commit_apply.go` `localCommitGit.output` injects `-c
  core.hooksPath=<hooksPath>` on `commit` (`:341-343`, exec at `:345`) but sets **no
  `cmd.Env`** (CORRECTION C-3). **Confirmed.**
- `verifier/receipt.go` `runOnce` builds a **minimal caller-supplied env** (PATH+HOME
  only, `:556-559`): the positive exemplar `gitEnv()` generalizes. **Confirmed.**
- The build already has an **AST-walking invariant** to mirror/extend:
  `TestDaemonMutationGitInvocationsDoNotUseCheckoutOrWorkingTreeMerge`
  (`mutations/git_invocation_guard_test.go:13-61`). **It already inspects helper call
  sites** (`runGitWorktreeCommand`/`integrateGit`, switching on `call.Args[2]`,
  `:49-53`) — the precedent §4 builds on. **Confirmed.**

The **four §0 corrections C-1..C-4 stand** (status.go is in `reads` not `mutations`;
the surface is larger than the RFC's "~6"; the commit path pins no `cmd.Env`; an
`os.Environ()`-sourced env is a real in-tree false-negative and `add`/`write-tree`
are gadget-bearing). v2 **extends C-2 and C-4**, it does not re-find them.

**CORRECTION C-2′ (the v1 undercount, now complete) — the daemon git surface is TWO
funnels plus direct execs, not one.** v1 enumerated the read funnel only. The
complete daemon-identity git surface, grep-verified on current source:

#### Funnel helpers (the chokepoints that must carry the closed env)

| Funnel | Def | exec | current env | subcommand guard |
|---|---|---|---|---|
| `localGit.output` | `reads/git_snapshot.go:193` | `:200` | **nil** | allowlist `rev-parse`/`status`/`log` |
| `localCommitGit.output` | `mutations/git_commit_apply.go:334` | `:345` | **nil** | `validateGitCommitArgs` + `-c core.hooksPath=` on commit |
| `readGitOutput` | `reads/worktree_refs.go:444` | `:446` | **nil** | none |
| `defaultRunGitWorktreeCommand` (= `runGitWorktreeCommand`) | `mutations/worktree.go:1603` | `:1604` | **nil** | **none — runs ANY subcommand** |
| `integrateGit` | `mutations/integrate.go:194` | wraps `runGitWorktreeCommand` | **nil** | none |
| `runGitWithEnv` | `mutations/recovery_quarantine_lane.go:440` | `:441` | **`os.Environ()`-sourced** (callers `:378`, `barrier_fanin.go:877`) | none |

`defaultRunGitWorktreeCommand` is the v1 blind spot: **no env, no subcommand guard,
`cmd.Dir = repoRoot`** (the attacker-controlled dir), and it is the single funnel for
`status`/`add`/`commit`/`diff`/`show`/`reset`/`for-each-ref`/`rev-parse`/`branch`/
`worktree`/`merge-tree`/`update-ref`/`rev-list`/`merge-base`.

#### Every daemon-identity call site, classified

Route class: **R** read-only/ref-plumbing · **S** status(fsmonitor) · **C**
commit(hooksPath) · **M** index-mutation(filter.clean) · **D** diff/render(textconv,
diff.external) · **W** worktree-admin · **L** lane-side · **X** CLI-only (operator/
agent process, **not** the daemon service identity).

| Site | Subcommand(s) | Via | Class | env now | P0 closure |
|---|---|---|---|---|---|
| `reads/git_snapshot.go:200` | status / rev-parse / log | `localGit.output` | R/**S** | nil | `gitEnv()` + `-c core.fsmonitor=` for status |
| `reads/doctor_artifact_anchor.go:537` | cat-file -e | direct | R | nil | `gitEnv()` |
| `reads/doctor_artifact_anchor.go:585` | show `<c>:<path>` (raw blob) | `readGitFileBytes` | R | nil | `gitEnv()` (no `--textconv` ⇒ no render gadget) |
| `reads/worktree_refs.go:424` | merge-base --is-ancestor | direct | R | nil | `gitEnv()` |
| `reads/worktree_refs.go:446` | rev-parse / for-each-ref / log / symbolic-ref | `readGitOutput` | R | nil | `gitEnv()` |
| `reads/doctor_barrier.go:575` | rev-parse --verify --quiet | direct | R | nil | `gitEnv()` |
| `reads/status.go:388` | branch --show-current | direct | R | nil | `gitEnv()` |
| `mutations/write_scope_guard.go:231` | status --porcelain -z | direct | **S** | nil | `gitEnv()` + `-c core.fsmonitor=` |
| `mutations/run.go:920/930/943/956` | branch --show-current / rev-parse --verify / branch | direct | R | nil | `gitEnv()` |
| `mutations/git_commit_apply.go:143/147/157` | add `--` / commit / rev-parse | `localCommitGit.output` | **M/C**/R | nil | `mutationEnv()`; commit already `-c core.hooksPath=`; add→filter-free staging |
| `mutations/git_commit_apply.go:345` | (the funnel exec) | — | — | nil | `mutationEnv()` |
| `mutations/git_commit_apply.go:393/405/413/421` | rev-parse / status | `localCommitGit.output` | R/**S** | nil | `gitEnv()`/`mutationEnv()` + `-c core.fsmonitor=` for `:421` |
| `mutations/recovery_quarantine_lane.go:425` | **status --porcelain** | `runGitWorktreeCommand` | **S** | nil | **`-c core.fsmonitor=` + `gitEnv()` — the LIVE RCE, A28** |
| `mutations/recovery_quarantine_lane.go:207/284` | worktree remove --force | `runGitWorktreeCommand` | W | nil | `mutationEnv()` |
| `mutations/recovery_quarantine_lane.go:378/386/391` | read-tree / **add -A** / write-tree | `runGitWithEnv` | R/**M** | **`os.Environ()`** | `mutationEnv()` (A31); add -A of attacker content → minted config (Slice 2) |
| `mutations/recovery_quarantine_lane.go:404` | commit-tree (plumbing, **no hooks**) | `integrateGit` | R | nil | `mutationEnv()` |
| `mutations/integrate.go:215`, `barrier_fanin.go:505/691/776/777/906/933/952` | merge-tree / update-ref / rev-list / merge-base / rev-parse | `integrateGit` | R | nil | `mutationEnv()` |
| `mutations/barrier_fanin.go:885` | commit-tree (no hooks) | `runGitWithEnv` | R | **`os.Environ()`** | `mutationEnv()` (A31) |
| `mutations/artifact_reconstructability.go:305/383/389/400` | for-each-ref / rev-parse / show `<c>:<path>` | `runGitWorktreeCommand` | R | nil | `gitEnv()` |
| `mutations/artifact_durability.go:138` / `artifact_source_publish.go:102` | **add -f -- `<path>`** | `runGitWorktreeCommand` | **M** | nil | **filter-free staging, A29** |
| `mutations/artifact_durability.go:152` / `artifact_source_publish.go:115` | diff --cached --quiet | `runGitWorktreeCommand` | **D** | nil | `mutationEnv()` + pinned argv (no `--textconv`/`--ext-diff`; `--quiet` short-circuits) |
| `mutations/artifact_durability.go:157` / `artifact_source_publish.go:120` | **commit** | `runGitWorktreeCommand` | **C** | nil | **`-c core.hooksPath=<empty>` + `mutationEnv()`, A30** |
| `mutations/artifact_durability.go:235` | show `HEAD:<path>` (raw blob) | `runGitWorktreeCommand` | R | nil | `gitEnv()` (no `--textconv`) |
| `mutations/worktree.go:118/181/192/200/297/634/732` | worktree add/remove / rev-parse / branch | `runGitWorktreeCommand` | W/R | nil | `mutationEnv()` |
| `mutations/worktree.go:1810` | archive --format=tar | direct | W | nil | `mutationEnv()` (archive honors `export-subst`/`export-ignore`, **no exec gadget**) |
| `mutations/repo_patch.go:186` | apply --whitespace=nowarn | `runGitApply` (direct) | M | nil | `mutationEnv()` (`apply` runs no clean/smudge driver) |
| `mutations/revision_routing.go:627` | reset --hard | `runGitWorktreeCommand` | W | nil | `mutationEnv()` |
| `verifier/receipt.go:606/609` | **add -A** / write-tree | direct | **M** | **`os.Environ()`** | `mutationEnv()` (A31); add -A of attacker content → minted config (Slice 2) |
| `agentloop/loop.go:268` | the driven agent command | direct | **L** | composed `childEnv` | born-neutralized lane env (§3.4, A10) |
| `agentloop/mcpconfig.go:349` | rev-parse --git-path info/exclude | direct | L | nil | lane `gitEnv()` (honors no exec config) |
| `cmd/striatum/operator_bootstrap.go:472` | (probe) | direct | **X** | nil | CLI/operator identity — out of daemon-invariant scope; named for completeness |
| `cmd/striatum/scope_check.go:264` | status --porcelain | direct | **X** | nil | CLI/operator identity — out of scope; named |

**A2 is the falsifiable exhaustiveness claim:** a tree-wide grep of
`exec.Command(Context)?("git"|g.path, …)` across the daemon packages plus the funnel
call sites, diffed against this taxonomy + the sanctioned `safegit` allowlist, finds
**no** daemon-identity read/ref/mutation git exec outside it. A 0-day site refutes A2
and fails the build (same mechanism as A12).

---

## §1 — The P0 boundary (the precise floor claim, the tested residual, the seams)

**P0 = Slice 0 (chokepoint seam over reads AND the mutation funnel) + Slice 1
(`gitEnv()`/`mutationEnv()` floor + the demoted fixed-`-c` interim + filter-free
staging + red-team corpus + compile-time invariant).** Carried from v1, **extended**
to the funnel.

**What P0 closes — by OMISSION (structural) for the whole surface:**

1. The ambient `GIT_*` gadget surface, the `GIT_CONFIG_COUNT`/`KEY_n`/`VALUE_n`
   family, and global/system config — never present in the closed env (`gitEnv()`/
   `mutationEnv()`), now set on **every** funnel/helper/direct exec, not just the read
   funnel. This also **closes the three `os.Environ()`-sourced false-negatives**
   (`recovery_quarantine_lane.go:378`, `barrier_fanin.go:877`, `receipt.go:605`).
2. The driven agent's own bare `git` (§3.4 lane env).
3. Structurally: the surface is collapsed under one compile-time invariant (§4) so an
   unknown future gadget no-ops by omission.

**What P0 closes by a DEMOTED fixed-key interim (non-boundary, in-repo-local):** the
**live LIVE RCEs** whose gadget key is **fixed and known**:

- `status`→`core.fsmonitor` at `git_snapshot.go:200`, `write_scope_guard.go:231`,
  `git_commit_apply.go:421`, and **`recovery_quarantine_lane.go:425`** — closed by
  `-c core.fsmonitor=` on the daemon's own status (A28).
- `commit`→`core.hooksPath` at `git_commit_apply.go` (already) and the porter commits
  `artifact_durability.go:157`/`artifact_source_publish.go:120` — closed by `-c
  core.hooksPath=<empty>` (A30).

**What P0 closes by FILTER-FREE STAGING (in-repo-local, arbitrary driver):** the
porter single-path `add -f -- <path>`→`filter.<driver>.clean` — staged via `git
hash-object --no-filters -w -- <path>` + `git update-index --add --cacheinfo
<mode>,<sha>,<path>`, which **never runs the clean filter** regardless of the
attacker's `.gitattributes` driver name (A29). No minted config required.

**What P0 does NOT yet close — stated honestly, tested as expected-fail vs L2:** the
**arbitrary in-repo-config OMISSION closure** for (a) the driven agent's arbitrary
`git diff`/`log -p`/`show --textconv` honoring in-repo `diff.external`/`*.textconv`,
and (b) the quarantine/receipt `add -A` / `write-tree` that stage **whole-tree
attacker content** (where filter-free per-path staging is impractical). These are
closed by **omission** only when Slice 2's minted `clean.git` (no `filter`/`diff`/
`textconv` keys) lands — and they are **expected-fail vs L2 / expected-pass vs Slice
2** rows in §5, surfaced not hidden. The demoted detector **refuses** (typed pre-exec,
→ human-cleared lane) any config-sensitive route it cannot prove neutralized, so
nothing in this residual **silently** executes (A20/A32).

**Precision (carried from v1, re-verified):** `core.pager`/`GIT_PAGER` are inert on
the daemon's captured-output reads (`cmd.Stdout = &buf`, no tty) and `GIT_PAGER` is
omitted; `git show <c>:<path>` and `cat-file` extract **raw blobs** (no `--textconv`
⇒ no render gadget); `git diff --cached --quiet` short-circuits on the exit-code
check and the chokepoint pins out `--textconv`/`--ext-diff`. So the real in-repo
residual is exactly the agent's arbitrary diff/render and the whole-tree `add -A`.

**Recommendation to the gate:** sequence Slice 2 immediately after P0; do not treat
P0 as the omission closure for in-repo config. P0 is a real, independently verifiable
floor that now **closes every live daemon-identity RCE** (fsmonitor-on-status incl.
the quarantine path, hooksPath-on-commit, filter-on-porter-add) and routes the whole
funnel through the closed env, ships the regression corpus, and is green under the
compile-time invariant.

### Falsifiable assertion

- **A0 (floor precision).** P0 neutralizes by omission the ambient/`GIT_CONFIG_COUNT`/
  global/system/agent-env classes **for the whole funnel**, closes the named live
  in-repo RCEs by the demoted interim + filter-free staging, and leaves exactly the
  arbitrary-in-repo-render + whole-tree-`add -A` residual bounded above. *Refuting
  test:* the §5 corpus shows a sentinel for any class P0 claims closed (refutes the
  floor), or a sentinel **not** created for a residual row P0 admits (residual
  mis-stated).

---

## §2 — Slice 0: the chokepoint seam over reads AND the mutation funnel

### Design

`CleanRepoFor(repoRoot, laneID) → (cleanRepoRoot, err)` stays **identity in Slice 0**
(`== (repoRoot, nil)`; zero behavior change, no truncated graph — §7, A3). The v2
extension: the **env/argv chokepoint is widened to the mutation funnel**. Concretely,
`safegit` owns three spawn primitives that every daemon git exec routes through:

```go
// go/pkg/safegit/spawn.go
func ReadSpawn(ctx, repoRoot, args...)            (out string, err error) // gitEnv() + read allowlist + fsmonitor interim
func MutateSpawn(ctx, repoRoot, opts, args...)    (res Result, err error) // mutationEnv() + commit/hooks + filter-free staging hook
func StageBlob(ctx, repoRoot, repoPath string)    (sha string, err error) // hash-object --no-filters + update-index --cacheinfo
```

- The six funnel helpers (§0) become **thin wrappers over `ReadSpawn`/`MutateSpawn`**;
  the direct execs route through them too. The funnel keeps its existing signatures so
  the ~30 call sites do not change shape — only the helper body changes to obtain its
  env (and the fsmonitor/hooks/filter handling) from `safegit`.
- **Exhaustiveness is grep-backed (A2) then compile-enforced (A4/A12).** The taxonomy
  is the test's allowlist; any new daemon git exec not routed through `safegit` fails
  the build.

### 2.2 Explicitly closing `recovery_quarantine_lane.go:425`

`quarantineWorktreeChangedPaths` runs `runGitWorktreeCommand(ctx, worktreeRoot,
"status", "--porcelain=v1", "-z", "--untracked-files=all")` against the **lane
worktree** (attacker-influenced, via its commondir config) with **nil env** —
firing in-repo `core.fsmonitor` as the daemon user: **a live RCE.** After v2 this
status (and every daemon `status`) routes through `ReadSpawn`, which sets `gitEnv()`
**and** appends `-c core.fsmonitor=`. The `quarantine_status_fsmonitor` corpus row
(§5) plants `core.fsmonitor='touch S'` in the worktree's effective config, drives the
exact quarantine code path, and asserts the sentinel is created on current source
(**red-before**) and absent after the fix (**green-after**) — A28.

### Falsifiable assertions

- **A1 (single chokepoint).** Every daemon git invocation (reads + the mutation
  funnel) obtains its env (and root) from `safegit`. *Refuting test:*
  `chokepoint_routing_test` (AST, §4 sibling) asserts no enumerated site spawns git
  with an env not sourced from `safegit`.
- **A2 (exhaustive taxonomy).** The §0 taxonomy is the complete daemon read/ref/
  mutation git surface. *Refuting test:* `git_surface_enumeration_test` greps the
  daemon packages + funnel call sites and diffs against the taxonomy + `safegit`
  allowlist; any unlisted site fails the build.
- **A3 (Slice 0 is behavior-neutral).** `CleanRepoFor ≡ identity`; the env/staging
  routing changes no answer on a benign repo. *Refuting test:* `cleanrepo_identity_test`
  + the full `reads`/`mutations` suites stay green; `read_parity_p0_test` (A21) and
  `stage_blob_parity_test` (A29) assert byte-equal results.
- **A4 (single sanctioned spawn surface).** No daemon `git` exec exists outside the
  three `safegit` primitives + the named X (CLI) exceptions. *Refuting test:* the §4
  invariant.

---

## §3 — Slice 1a: `gitEnv()` / `mutationEnv()` — the born-neutralized closed environment

### Design

```go
// go/pkg/safegit/gitenv.go
func gitEnv() ([]string, error)                       // reads: closed allowlist, NEVER os.Environ()
func mutationEnv(scratchIndexPath string) ([]string, error) // gitEnv() (+ GIT_INDEX_FILE when set), still closed
```

`gitEnv()` returns exactly this closed allowlist (carried from v1):

```
PATH=<daemon-pinned minimal PATH>
GIT_CONFIG_NOSYSTEM=1
GIT_CONFIG_GLOBAL=/dev/null
GIT_CONFIG_SYSTEM=/dev/null
HOME=<sacrificial empty daemon-owned dir>
XDG_CONFIG_HOME=<same sacrificial dir>
GIT_TERMINAL_PROMPT=0
LANG=C  LC_ALL=C  TZ=UTC
```

By construction: **OMISSION** of the `GIT_CONFIG_COUNT`/`KEY_n`/`VALUE_n` family
(load-bearing — omission *is* the neutralization, A7/A16) and of every gadget env var
(`GIT_EXTERNAL_DIFF`, `GIT_PAGER`, `GIT_SSH_COMMAND`,
`GIT_ALTERNATE_OBJECT_DIRECTORIES`, `GIT_DIR`, `GIT_WORK_TREE`, `GIT_PROXY_COMMAND`,
`GIT_ASKPASS`, …) — built from a **closed allowlist, not subtracted** from
`os.Environ()`.

**`mutationEnv(scratch)` = `gitEnv()` + (`GIT_INDEX_FILE=<scratch>` when staging to a
scratch index).** This is the structural fix for the `os.Environ()`-sourced
false-negatives: `recovery_quarantine_lane.go:378`, `barrier_fanin.go:877`, and
`receipt.go:605` currently do `append(os.Environ(), "GIT_INDEX_FILE=…")`, inheriting
the **entire daemon env including any ambient `GIT_*` gadget**. They switch to
`mutationEnv(scratch)` — closed, never `os.Environ()` (A31).

### 3.1 The bounded subcommand allowlist (split by primitive)

- **`ReadSpawn`:** `log, show, cat-file, rev-parse, status, for-each-ref, merge-base,
  rev-list, diff-tree, symbolic-ref, branch (--show-current only)`. `show`/`cat-file`
  are blob-extraction (no `--textconv`); `diff`/`log -p` rendering modes and
  `--textconv`/`--ext-diff` are refused.
- **`MutateSpawn`:** `commit (with -c core.hooksPath=), commit-tree, write-tree,
  read-tree, update-ref, update-index, merge-tree, branch, worktree, archive, reset,
  apply, diff --cached --quiet (no --textconv/--ext-diff)`. `add` is **not** spawned
  directly — index staging goes through `StageBlob` (filter-free) for single paths;
  whole-tree `add -A` is the §1 Slice-2 residual.
- Anything outside the sets (incl. `fetch`/`pull`/`push`/`remote`/`clone`/`submodule`)
  is refused pre-exec with a typed error.

### 3.2 Refuse, never degrade (carried, extended to `mutationEnv`)

```go
var ErrGitEnvUnavailable = errors.New("safegit: closed git environment unavailable; refusing bare exec")
```

If the closed env cannot be built, or the subcommand is not allowlisted, the
primitive returns the typed error and the caller fails closed. **No** `exec.Command`
path runs with `nil` or `os.Environ()` env — for reads **or** mutations (A8). A
gate-state `gate.read_gadget_blocked` (§8.3) is the recovery surface for this "could
not neutralize" condition.

### 3.3 The demoted fixed-`-c` interim (§1) — now over the whole status/commit surface

On the daemon's own captured-output spawns only: `ReadSpawn` appends `--no-pager -c
core.fsmonitor=` to **status**; `MutateSpawn` appends `-c core.hooksPath=<empty
tmpdir>` to **commit**. These are the RFC's demoted denylist (non-boundary,
telemetry-grade) — reliable because the closed env restores `-c` authority and the
keys are **fixed**. They die for arbitrary in-repo driver names (filter/textconv),
which Slice 2's minted config closes by omission.

### 3.4 The driven agent's lane env (carried)

`gitEnv()`'s pins compose into `childEnv` at `agentloop/loop.go:268`: NOSYSTEM/GLOBAL/
SYSTEM, sacrificial HOME/XDG, **omit** the `GIT_CONFIG_COUNT` family + gadget vars —
so a socially-engineered bare `git` is born-neutralized at the env layer (A10). It is
still exposed to in-repo local config until Slice 2 attaches the lane worktree to
`clean.git` (§1 residual, §9 seam).

### Falsifiable assertions

- **A6 (closed allowlist, not a subtraction).** `gitEnv()`/`mutationEnv()` output is
  exactly the pinned allowlist (plus `GIT_INDEX_FILE` for the latter) and contains no
  `os.Environ()` value. *Refuting test:* `gitenv_closed_test` sets `GIT_PAGER`,
  `GIT_EXTERNAL_DIFF`, `GIT_CONFIG_COUNT=…`, `LD_PRELOAD`, junk in the process env,
  asserts none appear and the result equals the pinned set.
- **A7 (`GIT_CONFIG_COUNT` family omitted — direct structural assertion).** *Refuting
  test:* `gitenv_no_config_count_test` asserts **directly** that neither `gitEnv()`,
  `mutationEnv()`, nor the composed lane `childEnv` contains any
  `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n` key — **not** merely that
  a sentinel was absent (closing Falsifier 1's "green for the wrong reason" note). The
  `env_config_count_*` corpus row (§5) is corroborating, not the sole proof.
- **A8 (refuse, never degrade — reads and mutations).** When the sacrificial dir is
  unstat-able, both `gitEnv()` and `mutationEnv()` return `ErrGitEnvUnavailable` and no
  git runs. *Refuting test:* `gitenv_refuses_test` over both primitives; a bare exec
  refutes A8.
- **A9 (bounded subcommands per primitive).** *Refuting test:*
  `spawn_subcommand_allowlist_test` drives `fetch`/`remote`/`submodule` (both
  primitives), `--textconv`/`--ext-diff` (ReadSpawn), `add -A` (MutateSpawn); each
  refused pre-exec.
- **A10 (lane env born-neutralized).** *Refuting test:* `lane_env_neutralized_test`
  asserts `childEnv` matches the floor; a gadget var or missing pin refutes A10.
- **A11 (`add` excluded from direct spawn).** *Refuting test:* asserting `MutateSpawn`
  refuses bare `add`; index staging must go through `StageBlob`.
- **A31 (no `os.Environ()`-sourced funnel env).** `runGitWithEnv` callers and
  `receipt.CwdTreeSHA` use `mutationEnv`, never `append(os.Environ(), …)`. *Refuting
  test:* a fixture with `cmd.Env = append(os.Environ(), …)` on a daemon git exec fails
  the §4 invariant; `os_environ_ban_test` covers the three named sites.

---

## §4 — Slice 1b: the compile-time invariant (every git call is neutralized, BY THE BUILD)

### Design — reconciling §4/A12 HONESTLY (the C1 bind)

Extend `git_invocation_guard_test.go` into `TestDaemonGitInvocationsAreNeutralized`
over the daemon packages (`reads`, `mutations`, `verifier`, the `agentloop` lane
spawn). It flags any `git`-spawning `exec.Command`/`exec.CommandContext` (first arg
`"git"` or a `*.path` receiver) **and any call to a funnel helper** that is not:

1. inside `safegit` (the sanctioned primitives), or
2. a call whose `cmd.Env` traces to `safegit.gitEnv()`/`mutationEnv()`/`LaneEnv()`
   (AST data-flow), **and** is **not** `os.Environ()`-sourced.

**The invariant inspects helper call sites — there is in-tree precedent.** The
existing guard already switches on `runGitWorktreeCommand`/`integrateGit` and reads
`call.Args[2]` (`git_invocation_guard_test.go:49-53`). v2 generalizes that: the funnel
helpers are the chokepoint, so the invariant requires each funnel **definition** to
obtain its env from `safegit`, and bans **direct** `exec.Command("git", …)` /
`append(os.Environ(), …)` in non-`safegit` daemon code. This is why an AST check over
raw `exec.Command` alone is insufficient (Falsifier 1's point) — and why v2 checks the
**helper boundary**, not just raw exec.

Because of C-4, the check is **not** a nil-test: `cmd.Env = os.Environ()` /
`append(os.Environ(), …)` **fails** the build (this pattern exists today at
`receipt.go:605` and `recovery_quarantine_lane.go:378`, so the invariant lands red and
forces the `mutationEnv` fix).

**Green-build reconciliation (the §11 claim made TRUE):** in P0, **every** funnel/
helper/direct daemon git exec routes its env through `safegit`. There is no daemon git
exec left with `nil`/`os.Environ()` env. Therefore the invariant is **green within the
§11 manifest in P0** — A2 and A12 are TRUE, **not retracted**. The honest residual is
the in-repo-config **omission** closure (minted config) for the arbitrary-driver
render + whole-tree `add -A` routes; that is a §5 expected-fail-vs-L2 row and a §9
Slice-2 seam — it does **not** make the invariant red, because those routes still set
a closed env (just not a minted config).

### Falsifiable assertions

- **A12 (compile-time completeness).** No daemon git exec runs with `nil`,
  `os.Environ()`-sourced, or non-`safegit` env outside the sanctioned primitives.
  *Refuting test:* `TestDaemonGitInvocationsAreNeutralized`; a planted
  `exec.Command("git","log")` with bare env, or a funnel helper reverted to nil env,
  must fail the build.
- **A13 (`os.Environ()` ban, not just nil).** *Refuting test:* a fixture with
  `cmd.Env = append(os.Environ(), "X=1")` is flagged.
- **A14 (funnel + commit path in scope).** The invariant covers
  `worktree.go:1604`, `recovery_quarantine_lane.go:441`, `git_commit_apply.go:345`,
  `receipt.go:606/609`. *Refuting test:* the invariant over `mutations`/`verifier`
  flags any of them while bare; green-while-bare refutes A14.
- **A27 (funnel routed through the closed env).** Every funnel helper definition
  obtains its env from `safegit`. *Refuting test:* `funnel_env_routing_test` asserts
  each of the six funnels spawns with a `safegit`-sourced env; a nil/`os.Environ()`
  funnel refutes A27 (and is the precondition for A28–A31).

---

## §5 — Slice 1c: the red-team corpus (the certificate, G4)

`go/pkg/reads/gate_corpus_test.go` + `go/pkg/mutations/funnel_corpus_test.go` — table
tests, one row per gadget. Each builds a temp repo / worktree / process-env condition
planting a `touch <sentinel>` payload, runs the **real** subcommand through the
relevant `safegit` primitive (and, for lane rows, the lane env), and asserts the
sentinel state. The green-result hash is `corpus_green_hash` (consumed by Slice-3
attestation, §8).

Carried-from-v1 rows (unchanged): `env_pager`, `env_external_diff`, `env_ssh_command`,
`env_config_count_pager`, `global_gitconfig_pager`, `system_gitconfig_fsmonitor`,
`inrepo_config_fsmonitor` (pass via the interim), and the expected-fail-vs-L2 residual
rows `inrepo_config_diff_external`, `inrepo_attributes_textconv`, `agent_bare_git_diff`
(→ expected-pass vs Slice 2). **New / changed rows:**

| Row | Plant | Subcommand / path | red-before | green-after | Closed by | A# |
|-----|-------|-------------------|-----------|-------------|-----------|----|
| **`quarantine_status_fsmonitor`** | worktree effective `core.fsmonitor='touch S'` | the `recovery_quarantine_lane.go:425` `status` path | **sentinel created** | **absent** | `gitEnv()` + `-c core.fsmonitor=` (§2.2) | A28 |
| **`porter_commit_hookspath`** | repo `core.hooksPath=<dir>/pre-commit='touch S'` | the `artifact_durability.go:157` porter `commit` | **sentinel created** | **absent** | `mutationEnv()` + `-c core.hooksPath=<empty>` | A30 |
| **`porter_add_filter_clean`** | `.gitattributes filter=pwn` + `filter.pwn.clean='touch S'` | the `artifact_durability.go:138` porter `add -f -- <path>` | **sentinel created** | **absent** | `StageBlob` (`hash-object --no-filters` + `update-index --cacheinfo`) | A29 |
| `quarantine_addA_filter_clean` | `.gitattributes filter=pwn` + `filter.pwn.clean='touch S'` | the `runGitWithEnv` `add -A` whole-tree snapshot | **sentinel created** | **expected-fail vs L2 / pass vs Slice 2 (minted config); refused-not-executed in P0** | minted config (Slice 2) / typed refusal (P0) | A18 |

The three **gate-named rows are P0-green** via concrete, source-anchored mechanisms;
they do not depend on Slice 2. `quarantine_addA_filter_clean` and the agent-diff
residuals are honest expected-fail-vs-L2 rows whose P0 behavior is **typed refusal**
(not silent execution), flipping to expected-pass when Slice 2's minted config lands.

### Falsifiable assertions

- **A15/A16/A17/A19 (carried).** Env/global/system gadgets no-op; `GIT_CONFIG_COUNT`
  precedence closed (corroborated by the row, *proven* by A7's direct assertion);
  fsmonitor-on-status interim works; `corpus_green_hash` deterministic.
- **A28 (`quarantine_status_fsmonitor`).** Red-before / green-after on the
  `recovery_quarantine_lane.go:425` path. A green-before or red-after refutes A28.
- **A29 (`porter_add_filter_clean`).** Red-before / green-after via `StageBlob`; plus
  `stage_blob_parity_test` asserts the staged blob equals `add`'s for a benign path
  (no behavior change, A3). A sentinel after the fix, or a content delta on benign
  input, refutes A29.
- **A30 (`porter_commit_hookspath`).** Red-before / green-after via the porter
  commit's `-c core.hooksPath=<empty>`. A fired hook after the fix refutes A30.
- **A18 (residual is exactly the arbitrary-render + whole-tree-add rows).** Those rows
  are expected-fail-vs-L2 and **refused (not executed)** in P0, expected-pass vs Slice
  2. An unexpected pass vs L2 means the residual is mis-modeled; an unexpected fail vs
  Slice 2 refutes the Slice-2 closure; a **silent execution** (no refusal) in P0
  refutes A20/A32.

---

## §6 — Hard core PROOF I: severance is COMPLETE (inert by omission)

Discharged route-by-route; a severance-completeness falsifier must find a route by
which a gadget still reaches exec.

| Route to exec | Closed by | Assertion |
|---------------|-----------|-----------|
| An **unrouted read OR funnel call site** | §2 chokepoint over reads + the mutation funnel; §4 invariant inspects **helper call sites** (taxonomy is the allowlist) | A1, A2, A4, A12, A27 |
| The **`runGitWorktreeCommand`/`integrateGit`/`runGitWithEnv` funnel** running gadget-bearing git with nil/`os.Environ()` env | §3 `gitEnv()`/`mutationEnv()` on every funnel definition; A31 closes the `os.Environ()` sources | A14, A27, A31 |
| **Live `recovery_quarantine_lane.go:425` `status`→fsmonitor** | §2.2 `-c core.fsmonitor=` + closed env | **A28** |
| **Porter `commit`→hooksPath** | `-c core.hooksPath=<empty>` on the funnel commit | **A30** |
| **Porter `add`→filter.clean** | `StageBlob` filter-free staging | **A29** |
| The **agent's own bare `git`** honoring env/global gadgets | §3.4 lane env | A10 |
| The **`GIT_CONFIG_COUNT` family** beating `-c` | §3 omission (direct assertion) | A7, A16 |
| **Ambient `GIT_*` / global / system** gadgets | §3 closed allowlist | A6, A15 |
| An **unenforced subcommand** | §3.1 per-primitive allowlist, refused pre-exec | A9, A11 |
| A **bare-exec fallback** when the closed env can't be built | §3.2 typed `ErrGitEnvUnavailable`, no fallback | A8 |
| An **unknown future gadget** | **omission**: absent from the closed env, call site chokepointed under the invariant | A0, A6, A12 |

**Honestly bounded residual (the one place omission is NOT yet achieved in P0):** the
**arbitrary in-repo render** (agent `git diff`/`show --textconv` honoring
`diff.external`/`*.textconv`) and the **whole-tree `add -A`** of attacker content. The
env floor cannot reach in-repo local config and the fixed-`-c`/`StageBlob` interims do
not generalize to an attacker-chosen driver over the whole worktree. The **omission**
closure is Slice 2's minted config (§9). In P0 these routes **refuse** (typed pre-exec
→ human-cleared lane), so nothing silently passes (A20/A32). This is §5's
expected-fail row, surfaced not hidden.

### Falsifiable assertion

- **A20 (no silent in-repo gadget exec).** No daemon git route executes an in-repo
  gadget without neutralizing it (env floor / fixed-`-c` / `StageBlob` / Slice-2
  minted config) **or refusing it** (typed pre-exec). *Refuting test:* the corpus is
  the closed enumeration; the Slice-3 `doctor` barrier flags any `git.*` exec lacking
  a preceding `gate.preflight_attested`. A silent pass refutes A20.

---

## §7 — Hard core PROOF II: severance is CORRECT (wrong answers, not just safety)

Carried from v1, unregressed. **P0 has NO truncated-graph risk:** `CleanRepoFor ≡
identity` in Slice 0, so every read runs against the **real** repository with full
objects/refs/alternates/accelerators intact. P0 changes only the **environment**, adds
the fixed-`-c` interim, and stages benign blobs filter-free — none of which alter
query *results*:

- `core.pager`/`GIT_PAGER`: output paging, inert on captured stdout.
- `diff.external`/textconv: diff **rendering**, not `rev-parse`/`merge-base`/
  `for-each-ref`/`log --format=%H` results.
- `core.fsmonitor`: a **performance accelerator**; disabling yields identical status.
- `StageBlob` (`hash-object --no-filters`) yields the **same blob** as `add` for any
  path with no legitimate clean filter (the only delta is when a filter would fire —
  exactly the attack), so the porter's committed content is unchanged on benign repos.
- In-repo local config is still read (real repo), so benign keys
  (`core.ignorecase`/`quotepath`/i18n) are preserved.

The **truncated-graph risk is entirely a Slice 2 concern** (minted objects-only
`clean.git`), specified now as the binding Slice-2 gate:

- **The parity harness (Slice-2 acceptance gate, A21b).** Before `clean.git` replaces
  the bare path, a corpus proves `clean.git` reads **equal** bare-repo reads over a
  matrix (`objects/info/alternates`, multi-pack indexes, packed-refs, per-worktree
  refs/commondir, shallow clones, `core.ignorecase`, gitdir-relative `core.worktree`)
  for `merge-base --is-ancestor`, `rev-parse --verify`, `for-each-ref`, `log
  --format=%H`, `cat-file -e`, `show <c>:<p>`. The minting step resolves alternates and
  carries all ref scopes; the benign-key allowlist is discovered empirically against
  the harness.

### Falsifiable assertions

- **A21 (P0 answer-equivalence).** For every chokepoint subcommand, the answer under
  the closed env + fixed-`-c` interim + `StageBlob` **equals** the prior bare-path
  answer over a benign-repo corpus. *Refuting test:* `read_parity_p0_test` +
  `stage_blob_parity_test`; a divergence refutes A21.
- **A21b (Slice-2 parity gate is named and binding).** `clean.git` does not replace
  the bare path until `read_parity_clean_git_test` is green. *Refuting test:* a Slice-2
  PR landing `clean.git` without a green parity harness refutes A21b.

---

## §8 — Discharged open design points (Slice-3 contracts frozen in P0)

Slice 3 (gate refs, attestation, recovery) is post-P0; its **contracts are frozen
here** so it cannot be built forgeable. Tests deferred to Slice 3; specifications are
P0 constraints.

### 8.1 Canonicalization golden vectors (carried from v1, unchanged)

`argv_digest`, `env_allowlist_digest`, `config_fingerprint` are **golden vectors
committed before any hashing code** (an under-specified canonicalization is a forgeable
digest): `argv_digest` = `sha256` over a length-prefixed, NUL-joined, order-preserving
argv encoding (binary path canonicalized to basename `git`, operands not normalized);
`env_allowlist_digest` = `sha256` over the **sorted** `KEY=VALUE` lines of the closed
env (HOME/XDG/PATH values redacted to a fixed token, others verbatim);
`config_fingerprint` = `sha256` over sorted `section.key=value` of the in-repo keys
*observed* (lowercased section/key, value verbatim). `golden_vectors_test` pins ≥3
hand-computed vectors per digest including adversarial cases (repeated `--format`,
case-only key diff, operand with `=`/NUL). **A22 carried, unregressed.**

### 8.2 The attestation contract + the decay-TOCTOU invariant (carried, surface widened)

- **Per-call attestation (Slice 3).** Before git forks, append
  `gate.preflight_attested{neutralizer_set@vN, set_hash, corpus_green_hash,
  argv_digest, env_allowlist_digest, config_fingerprint}`. A `git.*` exec with no
  immediately-preceding attestation is a hard `doctor` barrier failure. **The attested
  surface now enumerates the funnel exec sites** (§0) the v1 surface missed — so a
  funnel exec without attestation is itself a barrier failure (A25).
- **The decay-TOCTOU invariant (A23, carried).** Fingerprint decay may **clear the
  blocker** but must **never authorize the next call** — authorization always
  **re-attests at exec time**. This now lives on the `gate.read_gadget_blocked` state
  (§8.3), not on the benign-observed state.

### 8.3 The THREE-STATE recovery model (G3) — discharging C2

This is the C2 resolution: **one coherent state model, split by clearer-of-record**,
that makes a benign `[alias]`/`[pager]` **never wedge** while keeping
unknown/unattested keys on hard-refusal.

1. **`gate.read_gadget_observed` — NON-BLOCKING telemetry.** Emitted for a
   **recognized** key whose exec is **already neutralized by construction** (omitted
   from the closed env, run under the demoted fixed-`-c`, staged filter-free, or — for
   the residual — slated for the minted-config omission and meanwhile refused). It
   pins **no** `config_fingerprint` as a blocker, creates **no** recovery ref, and
   **never** blocks a job/run. A benign-but-retained `[alias] co=checkout` / `[pager]
   log=less -FRX` lands here. **Key point (answers the falsifier):** we do **not**
   classify the alias/pager *value* as benign-or-hostile (the falsifier correctly
   showed that is unsound) — a **recognized** key, benign or hostile, is **already
   inert**, so observation is purely informational. The demoted denylist becomes what
   the RFC says it is: telemetry, not the boundary.
2. **`gate.read_gadget_blocked` — the reserved BLOCKING state (A23 lives here).**
   Fired **only** for a condition that **genuinely could not be neutralized at
   preflight**: the closed env could not be built (`ErrGitEnvUnavailable`), or a
   config-sensitive mutation route reached exec before its P0 closure is wired (a real
   defect, not benign config). Machine-clearable by fingerprint decay; **exec
   re-attests** (A23). A stable benign config never reaches this state, so it never
   wedges.
3. **`gate.read_gadget_refused` — hard refusal into the human-cleared
   `recovery.quarantine_lane`.** For an **unknown/unattested** key — no taxonomy
   entry, no green-corpus coverage. A human adjudicates (`recovery
   accept-quarantined`). **No silent unknown-pass (A24/A32).**

All three grep under one `refs/striatum/` prefix; gate refs live under
`refs/striatum/gate/<run>/<job>/<attempt>` and are excluded from integrate/fan-in
sweeps via `NOT LIKE 'refs/striatum/gate/%'`. The blocker-vs-observability
contradiction is resolved: **`observed` is non-blocking by definition; `blocked` is
the only blocking state and carries the A23 decay semantics; `refused` is the human
lane.**

### Falsifiable assertions

- **A22 (unforgeable canonicalization — carried).** *Refuting test:* `golden_vectors_test`.
- **A23 (decay never authorizes — carried).** *Refuting test:* `decay_toctou_test` —
  clear a `blocked` pin, re-inject the unneutralizable condition, fork; assert the fork
  re-attests and re-blocks.
- **A24 (no silent unknown-pass; correct clearer-of-record).** Recognized+inert →
  non-blocking `observed`; genuinely-unneutralizable → machine-decay `blocked`;
  unknown → human-cleared `refused`. *Refuting test:* `three_state_recovery_test`
  drives one recognized-benign, one unneutralizable, one unknown; a benign one that
  blocks, or an unknown one that machine-clears or passes, refutes A24.
- **A25 (attestation precedes every exec — surface widened).** Every `git.*` exec —
  **including the funnel sites** — has an immediately-preceding `gate.preflight_attested`.
  *Refuting test:* the `doctor` barrier over a synthetic log with a missing funnel-exec
  attestation must flag it.
- **A26 (benign false-positive degrades observability ONLY, never wedges).** A
  benign-but-retained `[alias]`/`[pager]` yields at most a non-blocking
  `gate.read_gadget_observed` event and never a blocker. *Refuting test:*
  `false_positive_benign_test` (A33).
- **A32 (unknown key hard-refuses).** An unknown/unattested key routes to
  `gate.read_gadget_refused` → `recovery.quarantine_lane`; it never machine-clears or
  silently passes. *Refuting test:* the negative half of `false_positive_benign_test`.
- **A33 (`false_positive_benign_test` is load-bearing).** *Test (Slice 3, contract
  frozen now):* plant `[alias] co=checkout` + `[pager] log=less -FRX` in the target
  repo's local config; run an allowlisted read (`git log --format=%H`/`git status`)
  through Layer 3; assert **(1)** the read succeeds, **(2)** **no** job/run blocker is
  set, **(3)** **no** `recovery.quarantine_lane` ref is created, **(4)** at most a
  `gate.read_gadget_observed` (non-blocking) event exists, and **(5)** a **second**
  read proceeds with **no repo edit and no human clear**. Paired negative: plant an
  unknown/unattested key (a config key not in the §0 recognized taxonomy); assert it
  **hard-refuses** into the human-cleared lane (a `recovery.quarantine_lane` ref IS
  created and the read fails closed). Asserting only "no sentinel" (the v1 weakness)
  refutes A33.

---

## §9 — P0 boundary, named seams, and Non-Goals honored

**In P0 (Slice 0 + Slice 1):** the chokepoint seam over **reads and the mutation
funnel** (`CleanRepoFor ≡ identity`, `safegit` env/staging primitives), the
`gitEnv()`/`mutationEnv()` omission floor + the demoted fixed-`-c` interim
(fsmonitor/hooks) + `StageBlob` filter-free staging, the lane-env neutralization, the
red-team corpus (incl. the three funnel rows + the typed-refusal residual rows), and
the compile-time invariant (over helper call sites).

**Named build-bearing seams P0 leaves:**

- **Slice 2 (Layer 1, recommended to immediately follow).** `CleanRepoFor` mints &
  caches the objects-only `clean.git` (hardlink-shared objects + all ref scopes +
  resolved alternates + read-only accelerators; minted config from a fixed template,
  **no `filter`/`diff`/`textconv`/`hooks`/`alias`/`insteadOf`/`fsmonitor` keys**).
  Flips the §5 expected-fail rows (`inrepo_config_diff_external`,
  `inrepo_attributes_textconv`, `agent_bare_git_diff`, `quarantine_addA_filter_clean`)
  to expected-pass. **Gated on the §7 parity harness (A21b).**
- **Slice 3 (Layer 3).** `refs/striatum/gate/` subtree, `gate.preflight_attested` over
  the **full funnel surface**, the **three-state** recovery (observed / blocked /
  refused), and the `doctor` barrier — consuming the §8 frozen contracts.

**Non-Goals honored:** no agent arbitrary-command sandbox (RFC 0096); no
write-confinement rebuild / repo-config validator (`ValidateSandboxJail`, RFC 0127 —
SKIPs); no cross-lane learned immunity; local-first boundary intact (`clean.git`, gate
refs, recovery are daemon-owned local state under `$STATE`/`refs/striatum/`; no hosted
service, telemetry export, or external persistence).

---

## §10 — Assertion ledger (the falsifiers' target list)

| A# | Claim | Refuting test/row | Status vs v1 |
|------|-------|-------------------|------|
| **A0** | P0 floor precisely the env/global/system/agent classes by omission **over the funnel**; named in-repo RCEs closed; arbitrary-render + whole-tree-add the tested residual | §5 row expectations | revised |
| **A1** | Single chokepoint over reads **and** the mutation funnel | `chokepoint_routing_test` | revised |
| **A2** | The §0 taxonomy is the **complete** daemon read/ref/mutation git surface | `git_surface_enumeration_test` | **revised (C1)** |
| **A3** | Slice 0 behavior-neutral (`CleanRepoFor ≡ identity`; env/staging change no answer) | `cleanrepo_identity_test` + parity tests | revised |
| **A4** | No daemon git exec outside the `safegit` primitives + named X exceptions | the §4 invariant | new |
| **A6** | `gitEnv()`/`mutationEnv()` closed allowlist, no `os.Environ()` leak | `gitenv_closed_test` | revised |
| **A7** | `GIT_CONFIG_COUNT` family omitted — **direct structural assertion** | `gitenv_no_config_count_test` | **strengthened** |
| **A8** | Refuse (typed error), never bare-exec fallback — reads **and** mutations | `gitenv_refuses_test` | revised |
| **A9** | Per-primitive bounded subcommand allowlist, refused pre-exec | `spawn_subcommand_allowlist_test` | revised |
| **A10** | Lane env born-neutralized | `lane_env_neutralized_test` | carried |
| **A11** | `add` excluded from direct spawn (staging via `StageBlob`) | spawn refusal test | revised |
| **A12** | Compile-time completeness over **helper call sites**; no nil/`os.Environ()` env | `TestDaemonGitInvocationsAreNeutralized` | **revised (C1)** |
| **A13** | `os.Environ()`-sourced env rejected, not just nil | invariant fixture | carried |
| **A14** | Funnel + commit path in invariant scope | invariant over `mutations`/`verifier` | revised |
| **A15** | Env/global/system gadgets no-op | `env_*`/`global_*`/`system_*` rows | carried |
| **A16** | `GIT_CONFIG_COUNT` precedence closed | `env_config_count_*` row (corroborating A7) | carried |
| **A17** | Demoted interim kills fsmonitor-on-status | `inrepo_config_fsmonitor` row | carried |
| **A18** | Residual is exactly the arbitrary-render + whole-tree-add rows (expected-fail vs L2, **refused not executed** in P0, expected-pass vs Slice 2) | `inrepo_*` / `agent_bare_git_diff` / `quarantine_addA_filter_clean` rows | revised |
| **A19** | `corpus_green_hash` deterministic | corpus run-twice | carried |
| **A20** | No silent in-repo gadget exec (neutralize **or refuse**) | corpus closure + Slice-3 `doctor` barrier | revised |
| **A21** | P0 answer-equivalence (env floor + interim + `StageBlob` change no result) | `read_parity_p0_test` + `stage_blob_parity_test` | revised |
| **A21b** | Slice-2 `clean.git` gated on a green parity harness | Slice-2 landing | carried |
| **A22** | Unforgeable canonicalization (golden vectors) | `golden_vectors_test` (Slice 3) | carried |
| **A23** | Decay clears the blocker but never authorizes (re-attest) — on the `blocked` state | `decay_toctou_test` (Slice 3) | carried |
| **A24** | Three-state recovery never silently passes; benign→observed, unneutralizable→blocked, unknown→refused | `three_state_recovery_test` (Slice 3) | **revised (C2)** |
| **A25** | Attestation precedes every exec — **funnel surface included** | `doctor` barrier (Slice 3) | revised |
| **A26** | Benign `[alias]`/`[pager]` false-positive degrades observability ONLY, never wedges | `false_positive_benign_test` | **revised (C2)** |
| **A27** | Every funnel helper definition spawns with a `safegit`-sourced env | `funnel_env_routing_test` | **new (C1)** |
| **A28** | `quarantine_status_fsmonitor` red-before/green-after (`recovery_quarantine_lane.go:425`) | the row | **new (C1)** |
| **A29** | `porter_add_filter_clean` red-before/green-after (`StageBlob`); staged blob == `add` on benign | the row + `stage_blob_parity_test` | **new (C1)** |
| **A30** | `porter_commit_hookspath` red-before/green-after (`-c core.hooksPath=`) | the row | **new (C1)** |
| **A31** | No `os.Environ()`-sourced funnel env (the three named sites use `mutationEnv`) | `os_environ_ban_test` | **new (C1)** |
| **A32** | Unknown/unattested key hard-refuses into the human-cleared lane | `false_positive_benign_test` (negative) | **new (C2)** |
| **A33** | `false_positive_benign_test` load-bearing (no blocker, no recovery ref, second read clean; paired unknown-key refusal) | the test | **new (C2)** |

---

## §11 — Build manifest (P0) — the green-build claim, made TRUE

| Artifact | File | ~LOC | Tests |
|----------|------|------|-------|
| Chokepoint + spawn primitives | `go/pkg/safegit/cleanrepo.go` (`CleanRepoFor`), `safegit/spawn.go` (`ReadSpawn`/`MutateSpawn`/`StageBlob`) | ~120 | `cleanrepo_identity_test`, `chokepoint_routing_test`, `stage_blob_parity_test` |
| Closed envs | `go/pkg/safegit/gitenv.go` (`gitEnv`, `mutationEnv`, `ErrGitEnvUnavailable`, allowlists) | ~80 | `gitenv_closed_test`, `gitenv_no_config_count_test`, `gitenv_refuses_test`, `spawn_subcommand_allowlist_test` |
| Lane-env compose | edit `go/pkg/agentloop/loop.go:268` | ~10 | `lane_env_neutralized_test` |
| Read-funnel routing | `git_snapshot.go`, `worktree_refs.go`, `doctor_artifact_anchor.go`, `doctor_barrier.go`, `reads/status.go`, `run.go` → `safegit` | ~40 | existing `reads` suites stay green (A3) |
| **Mutation-funnel routing** | `worktree.go:1603`, `integrate.go:194`, `recovery_quarantine_lane.go:440/378`, `git_commit_apply.go:334/345`, `barrier_fanin.go:877`, `verifier/receipt.go:606/609`, porter `artifact_durability.go`/`artifact_source_publish.go`, `write_scope_guard.go` → `safegit`/`StageBlob`/`mutationEnv` | ~90 | `funnel_env_routing_test`, `os_environ_ban_test`, existing `mutations`/`verifier` suites green |
| Compile-time invariant (over **helper call sites**) | extend `go/pkg/mutations/git_invocation_guard_test.go` → `TestDaemonGitInvocationsAreNeutralized` | ~120 | the invariant (A12–A14, A27, A31) |
| Read-side corpus | `go/pkg/reads/gate_corpus_test.go` | ~240 | the carried §5 rows + `corpus_green_hash` |
| **Funnel corpus** | `go/pkg/mutations/funnel_corpus_test.go` | ~160 | `quarantine_status_fsmonitor`, `porter_add_filter_clean`, `porter_commit_hookspath`, `quarantine_addA_filter_clean` (A28–A30, A18) |
| P0 parity | `go/pkg/reads/read_parity_p0_test.go` | ~80 | `read_parity_p0_test` (A21) |
| Golden vectors (contract, consumed Slice 3) | `go/pkg/safegit/golden_vectors_test.go` | ~60 | `golden_vectors_test` (A22) |

**Green-build truth:** with every funnel/helper/direct daemon git exec routed through
`safegit`, `TestDaemonGitInvocationsAreNeutralized` is **green in P0** — A2/A12 are
TRUE within this manifest, not retracted. The honest residual is the in-repo-config
**omission** closure (minted config), which is a named Slice-2 seam and does not make
the invariant red (those routes still carry a closed env, and are **refused** in P0
rather than executed).

**This is the published v2 claim the falsifiers re-attack.** Falsifier 1
(C1/severance-completeness): re-grep the tree for any daemon-run git exec outside the
§0 taxonomy + `safegit` allowlist; verify `recovery_quarantine_lane.go:425` is closed
(A28); verify the three funnel rows go red-before/green-after (A28–A30); verify the
invariant inspects helper call sites and the §11 build is genuinely green (A2/A12), or
that I retracted honestly. Falsifier 2 (C2/false-positive + carry-forward): try to
wedge a benign `[alias]`/`[pager]` by any route (A26/A33); check the observed/blocked/
refused state model is coherent and `false_positive_benign_test` is real; confirm an
unknown key still hard-refuses (A32); then verify NO carry-forward regressed — layered
severance, A7/A8/A16, A21/A21b, A22/A23/A25, and the §0 corrections C-1..C-4.
