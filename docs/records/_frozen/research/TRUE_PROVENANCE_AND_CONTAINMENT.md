# Achieving True Provenance: Operator Containment Strategies

Status: exploratory research
Date: 2026-05-10

## The Problem

RFC 0026 ("Lane Attestation and Operator Byline Honesty") secured the *bottom* of the workflow. It ensures that if an artifact claims to be written by an agent lane, a `striatum`-supervised process actually executed the job. 

However, this leaves a massive loophole at the *top* of the workflow.

When a human delegates orchestration to a cloud-based **frontier model** running in its own optimized coding harness (e.g., Gemini CLI, Claude Code, or Codex), that AI acts as the **Operator**. These models are frequently fine-tuned for their specific harnesses and use a **Striatum Driver Plugin** to interact with the local control plane.

While the orchestrator is **local-first** (storing state, enforcing gates, and managing the repository locally), the inference is remote. The AI Operator has access to its own harness-native coding tools (`replace_file`, `write_file`, `run_shell_command`) which often bypass the local plugin. 

If tasked with "Use Striatum to fix the parser bug," the AI operator might optimize for speed and token efficiency. It might use its native tools to edit the parser code directly, completely bypassing the multi-lane review, the adversarial checks, and the entire Striatum local control plane—all while using the plugin to record a "success" event.

To achieve "true provenance"—where every line of code can be cryptographically traced back to a specific, attested workflow job, allowing us to safely remove the human from the loop—we must move from "advisory" plugin adherence to **hard containment** for the AI operator's native harness.

Below are four novel strategies to mitigate this operator-bypass problem, ranging from cryptographic enforcement to OS-level isolation.

---

## Strategy 1: The Cryptographic Provenance Ledger (Git Hooks)

**Concept:** Allow the operator to do whatever it wants, but categorically refuse to commit unauthorized changes.

**Mechanism:**
1. Striatum maintains an `attestation_ledger` in SQLite.
2. Whenever an *attested lane* (per RFC 0026) successfully completes a job, Striatum hashes the files modified within that job's `write_scope` and records the SHA256 hashes in the ledger alongside the `job_id`.
3. A strict `pre-commit` hook is installed in the repository.
4. When `git commit` is run, the hook hashes the staged files. If *any* file hash does not exist in the `attestation_ledger`, the commit is rejected with a loud error: *"Provenance Violation: File modifications bypass Striatum workflow."*

**Why it works:** The AI operator might try to cheat and edit files directly, but it will repeatedly fail to commit them. The error message explicitly forces the AI to back up, write the RFC, scaffold the workflow, and let the attested lanes generate the authorized artifacts.

**Pros:** Easy to implement. Enforces provenance at the version-control boundary.
**Cons:** Token-inefficient if the AI wastes time making unauthorized edits before hitting the commit wall.

---

## Strategy 2: OS-Level Write-Scope Isolation (The Sandbox)

**Concept:** Physically prevent the top-level operator from modifying source code.

**Mechanism:**
1. The repository's source directories (`src/`, `tests/`) are made read-only at the filesystem level for the standard user session where the AI operator runs. The operator only has write access to `docs/rfcs/`, `.striatum/`, and `workflow.json`.
2. Striatum's process adapters (which spawn the agent lanes) are given elevated privileges (e.g., via `bwrap`, user namespaces, or specialized Docker mounts).
3. When a job starts, Striatum dynamically provisions a read-write mount overlay strictly limited to that job's `allowed_paths` within the `write_scope`.

**Why it works:** The top-level operator is physically contained. If it attempts to use a tool like `replace_file` on `src/parser.py`, the OS returns `EACCES (Permission denied)`. It is forced to orchestrate the workflow to effect change.

**Pros:** Hard cryptographic-level security. Completely stops bypass attempts at the OS level.
**Cons:** High complexity. Requires Linux user-namespace configuration (`bwrap`) or containerization, complicating the "local-first, simple CLI" philosophy.

---

## Strategy 3: The "Blind Operator" (Shadow Worktrees)

**Concept:** The operator cannot modify code it cannot see.

**Mechanism:**
1. The operator AI is spawned inside a "Control Worktree"—a directory that *only* contains `docs/rfcs/`, `workflows/`, and the `.striatum/` control plane.
2. The actual source code (`src/`) is absent from this directory.
3. To understand the codebase, the operator is provided with special read-only API tools (`striatum_read_source`, `striatum_grep_source`).
4. Striatum's job adapters map the control worktree and the hidden source worktree together when spawning a lane, so the sub-agents see the full repository.

**Why it works:** "Out of sight, out of mind." The operator AI lacks the context to natively edit the code and lacks the file paths to apply tools to. It must write an RFC and a workflow to instruct the sub-agents (who *can* see the code) to do the work.

**Pros:** Very effective psychological/contextual containment for LLMs.
**Cons:** Creates friction for the operator when researching the RFC or scaffolding the workflow, as it must use clunky bridge tools to read the source.

---

## Strategy 4: Inverted Control Plane (Striatum Native Chat)

**Concept:** Remove the external AI operator entirely. Striatum becomes the primary shell.

**Mechanism:**
1. We elevate the RFC 0023 Web Chat (or introduce a new `striatum chat` TUI) to be the *exclusive* interface the human uses to interact with the project.
2. The AI assistant running inside Striatum is granted a strictly curated set of tools: `propose_rfc`, `scaffold_workflow`, `start_run`, `view_status`.
3. Crucially, the Striatum chat agent is **deprived** of `edit_file`, `replace`, or `write_file` tools for the main source code.

**Why it works:** If you want to achieve true provenance, don't give the orchestrating AI the tools to cheat in the first place. The human says, "Implement X." The Striatum agent, lacking file-editing tools, uses `propose_rfc` and `scaffold_workflow` because those are the *only* ways it can fulfill the user's intent. The workflow is then executed, spawning sub-agents (who *do* have file-editing tools) to do the work under the watchful eye of the deterministic coordinator.

**Pros:** Cleanest architectural solution. Perfectly aligns with Striatum's domain-driven design. Eliminates the need for external CLI wrappers.
**Cons:** Requires users to abandon their preferred general-purpose CLIs (Claude Code, Gemini CLI) when working on Striatum-managed projects.

---

## Conclusion & Proposed Roadmap

To achieve a fully autonomous, zero-human-in-the-loop pipeline with perfect provenance, we must adopt a layered containment strategy:

1. **Phase 1 (Immediate): Implement Strategy 1 (Cryptographic Ledger + Git Hooks).** This establishes a hard boundary at the commit level, ensuring that even if an operator cheats, the repository history remains pure.
2. **Phase 2 (Medium Term): Implement Strategy 4 (Inverted Control Plane).** Invest heavily in the RFC 0023 chat surface, turning it into a first-class Orchestrator Agent. By controlling the toolset, we naturally channel the AI's behavior into workflow generation.
3. **Phase 3 (Long Term): Implement Strategy 2 (OS Isolation).** Once the Orchestrator Agent is mature, isolate the agent lanes using `bwrap` to guarantee that write-scopes are enforced at the kernel level, achieving absolute cryptographic and physical provenance.