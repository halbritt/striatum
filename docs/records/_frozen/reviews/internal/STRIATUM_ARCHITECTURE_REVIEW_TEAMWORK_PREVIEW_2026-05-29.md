# STRIATUM EXPERT SYSTEMS ARCHITECTURE REVIEW

**Date of Review**: 2026-05-29  
**Reviewer Archetype**: Specialist Systems Architect (sentinel-worker-m2)  
**Target Repository**: `/home/halbritt/git/striatum`  
**Boundaries & Constraints**: Local-First, Single-Operator, Laptop/Homelab Runtime Scale  

---

## 0. Files reviewed

The following list represents the authoritative index of all primary codebase files audited during this architectural review:

*   `README.md`
*   `AGENTS.md`
*   `Makefile`
*   `docs/reference/spec.md`
*   `docs/reference/command-authority-matrix.md`
*   `docs/how-to/postgres-transition.md`
*   `docs/reference/domain-driven-design.md`
*   `go/cmd/striatum/main.go`
*   `go/cmd/striatumd/main.go`
*   `go/pkg/rpc/server.go`
*   `go/pkg/rpc/auth_pg.go`
*   `go/pkg/db/migrations.go`
*   `go/pkg/db/sql/0005_repo_local_workflow_state.sql`
*   `go/pkg/mutations/artifact.go`
*   `go/pkg/mutations/supervision_control.go`
*   `go/pkg/supervisor/helper.go`
*   `go/pkg/supervisor/pty.go`
*   `go/pkg/mcp/http.go`

---

## 1. Executive summary

*   **Complete Port Parity**: Striatum has executed a total cutover to a Go-only runtime environment under RFC 0078. The legacy Python runtime has been completely retired, and all system logic resides in native Go binaries (`striatum`, `striatumd`, `striatum-supervisor-helper`).
*   **Centralized State Store**: Pursuant to D094 / RFC 0043, SQLite and repo-local state files are retired. All workflow state resides in a daemon-owned PostgreSQL database, ensuring strict transactional boundaries.
*   **Loopback Security Guardrails**: The Model Context Protocol (MCP) server enforces a zero-trust posture on localhost. The HTTP interface actively rejects non-loopback Origin and Host headers with a 403 Forbidden status, shielding the system against cross-origin scripting and DNS rebinding exploits.
*   **Anti-Recycling Supervision**: The supervisor liveness engine uses Linux kernel boot-tick start times (`/proc/<pid>/stat` field 22) coupled with active tmux pane attestation to uniquely identify process lanes, completely eliminating PID recycling vulnerabilities.
*   **Cryptographic Append-Only History**: Database tables holding the core events and artifacts are locked down using native PL/pgSQL triggers and database-role privileges (`striatumd_rw`) that revoke `UPDATE` and `DELETE` access.
*   **Decoupled Process Execution**: Subprocess management is isolated inside `striatum-supervisor-helper` which operates process PTY streams and stdin/stdout FIFO pumps without importing database pools or RPC packages.
*   **Strong Test Posture**: The repository maintains an exceptionally strong test posture. Unit tests across all Go packages execute cached runs instantly, governed by a 20.0% coverage floor and dynamic RPC handler coverage tests.

---

## 2. What the project is trying to be

### Product Goals & Vision
Striatum is a standalone, local-first workflow coordinator for terminal-based AI coding agents. Unlike typical SaaS workflow engines or cloud-dependent pipeline runners, Striatum operates entirely within the boundaries of the operator's local workstation. The system does not attempt to be an LLM platform or a code generator itself. Rather, it serves as a secure, transaction-safe coordinator that orchestrates multi-agent feedback loops (implement, build, test, review, repair, synthesize) across isolated execution lanes.

### Architectural Principles
The project is built around three core architectural tenets:
1.  **Durable Provenance vs. Live State**: The live state of runs, sessions, and leases is stored in a transactional database (PostgreSQL), while the durable history of decisions and outcomes is committed directly to the target repository as human-readable Markdown documents with schema-validated YAML front-matter. This ensures that a repository is self-documenting, even if the database is destroyed.
2.  **Adversarial and Evidential Posture**: Striatum treats terminal-based AI agents as untrusted processes. It enforces directory-level write boundaries, prevents unauthorized tool invocation, and requires explicit review gates (verdicts) before allowing an agent's changes to progress to downstream execution nodes.
3.  **Local-First Isolation**: Execution processes are sandboxed via virtual pseudo-terminals (PTYs), custom tmux sessions, and Named Pipes (FIFOs). Telemetry, hosted SaaS databases, and external cloud integrations are actively rejected in favor of zero-dependency local utilities (e.g. local S3-compatible endpoints for asset blobs under RFC 0072).

### Domain and Operating Model
Striatum's domain boundaries are carefully defined under Bounded Context guidelines:
*   **Aggregate Roots**: The core state revolves around the `Run` (an instance of a workflow), the `Session` (the active shell environment representing an agent's workspace connection), the `Job` (the physical execution task mapped to a state-machine node), and the `Lease` (a time-locked exclusive hold on a job).
*   **Materialized Projections**: Introspective operations are materialized projections computed on-demand from an append-only transaction ledger rather than mutable state columns.
*   **Operating Model**: The background daemon `striatumd` operates as the single source of truth and system authority. It owns the PostgreSQL database connection, manages capability token grants, evaluates MCP tool requests, and monitors process lane liveness. The CLI tool `striatum` acts exclusively as an unprivileged client that communicates with the daemon over local Unix domain sockets.

---

## 3. Current architecture

The active codebase is an exceptionally clean, well-factored Go monorepo that encapsulates CLI tools, a system daemon, embedded SQL migrations, and compiled frontend assets. The diagram below illustrates the runtime topography:

```
                  ┌──────────────────────────────────────────────┐
                  │                 Local Browser                │
                  └──────┬────────────────────────────────┬──────┘
                         │                                │
                         │ HTTP (Dashboard)               │ SSE (Events)
                         ▼                                ▼
┌───────────┐    ┌────────────────────────────────────────────────┐
│   Local   │    │                 striatumd                      │
│    CLI    │    │                (Background)                    │
│           │    │                                                │
│           │    │ ┌───────────────┐ ┌───────────────┐ ┌────────┐ │
│ ┌───────┐ │    │ │  UNIX Socket  │ │  HTTP / SSE   │ │ S3 /   │ │
│ │Socket │ │    │ │  RPC Server   │ │  MCP Server   │ │ MinIO  │ │
│ │Client │ │    │ └───────┬───────┘ └───────┬───────┘ └────┬───┘ │
└─┴───┬───┴─┘    └─────────┼─────────────────┼──────────────┼─────┘
      │                    │                 │              │
      │ UNIX Socket        │                 │ HTTP         │ API
      │ (daemon-go.sock)   │                 │              │
      ▼                    ▼                 ▼              ▼
┌──────────────────────────┴─────────────────┴──────────────┴─────┐
│                       PostgreSQL Database                       │
│      (Schemas: striatumd, Tables: runs, sessions, audit_log)    │
└──────────────────────────────────┬──────────────────────────────┘
                                   │
                                   │ Spawns PTY
                                   ▼
                ┌──────────────────────────────────────┐
                │      striatum-supervisor-helper      │
                │        (Decoupled subprocess)        │
                └──────────────────┬───────────────────┘
                                   │
                                   ▼
                ┌──────────────────────────────────────┐
                │          Target Agent Lane           │
                │    (Isolated process / tmux pane)    │
                └──────────────────────────────────────┘
```

### Components and Runtime Boundaries
1.  **The CLI (`striatum`)**: Exposes commands to the user. It either runs purely local, stateless operations or dispatches JSON-RPC packets over Unix sockets to the daemon.
2.  **The Daemon (`striatumd`)**: Maintains a connection pool to PostgreSQL, hosts the JSON-RPC Unix socket server, operates the loopback MCP server, runs the web dashboard service, and schedules the background recovery sweeper.
3.  **The Supervisor Helper (`striatum-supervisor-helper`)**: A lightweight binary spawned by the daemon that interacts directly with the operating system's PTY subsystem. It reads control packet streams on stdin and forwards them to the running agent, while capturing stdout/stderr bytes and piping them back as structured event lines.

---

### Tri-Voice Grounding Analysis

#### A. UNIX Socket Boundary and Handshake Constraint
*   **Stated**: The system architecture mandates that the CLI operates exclusively as an RPC client to the background daemon, requiring connection-level handshake execution before any normal operational command can route. This is defined in `docs/reference/spec.md` under "Client-Daemon RPC Protocol" and detailed in the authority matrix of `docs/reference/command-authority-matrix.md`.
*   **Actual**: In `go/pkg/cli/rpcclient/client.go:79-88`, the socket dialer is hardcoded to issue a synchronous `daemon.hello` JSON-RPC call immediately upon connection. In `go/pkg/rpc/server.go:79-81`, the server checks `requireHandshake` and actively rejects any non-hello requests on a new connection with a `version_incompatible` error, failing the CLI execution closed with exit code 11.
*   **Mine**: Enforcing a strict, blocking handshake at the socket protocol layer is an exceptionally robust way to prevent client-daemon version drift. In local-first workflows, operators frequently upgrade binaries or leave old CLI windows running. The handshake ensures the client binary matches the daemon's runtime envelope capabilities, failing closed immediately rather than allowing malformed payloads to corrupt database states.

#### B. Dynamic Postgres-Backed Capability Authorization
*   **Stated**: Access to daemon mutations is locked down per scope and capability as specified in `docs/reference/command-authority-matrix.md`.
*   **Actual**: In `go/pkg/rpc/auth_pg.go:43-110`, `PostgresAuthorizer` performs a db-backed capability check: queries `striatumd.clients` (lines 61-66) for salts, checks signatures via `subtle.ConstantTimeCompare` (lines 73-76), and queries `striatumd.client_capabilities` (lines 88-101) to verify scopes.
*   **Mine**: While cryptographic validation is robust, executing synchronous DB queries on every RPC transaction introduces serialization overhead for fast-moving agents. The daemon should utilize an in-memory capability cache with a short TTL (e.g. 5 seconds) to alleviate SQL query pressure.

#### C. Model Context Protocol Loopback Guardrails
*   **Stated**: The Model Context Protocol (MCP) server must expose database workflow mutations as actionable tools while maintaining loopback isolation as specified in `docs/reference/spec.md`.
*   **Actual**: In `go/pkg/mcp/http.go:541-550`, `validateLocalRequest` rejects non-loopback Host/Origin headers with 403 Forbidden status. Furthermore, `go/pkg/mcp/capabilities.go:60-74` hides all workflow authoring tools, and `go/pkg/mcp/tools.go:34-37` intercepts unauthorized calls, failing closed with a `tool_hidden` error.
*   **Mine**: Loopback validation elegantly shields the daemon from cross-origin drive-by exploits. To prevent port-collision hijacking on homelabs, the daemon should transition from static port bindings to dynamic port allocation, publishing active ports to an encrypted local file in `.striatum/`.

#### D. Attested Process Lane Supervision
*   **Stated**: Process supervision must securely assert that the active process executing a job is indeed the registered agent shell, and not a recycled PID, as specified in `AGENTS.md`.
*   **Actual**: In `go/pkg/supervisor/process_identity_linux.go:13-32`, the system calls `ProcessStartToken(pid)` to read boot ticks from `/proc/<pid>/stat` field 22. In `go/pkg/supervisor/tmux_liveness.go:141-206`, the daemon validates this token, deriving verified bylines like `author: <role>-<model>-<ordinal>` or falling back to `author: operator` if unverified.
*   **Mine**: Coupling Unix PIDs with immutable boot ticks is a brilliant mechanism that solves the classic PID recycling TOCTOU vulnerability. However, this is currently hardcoded for Linux. To support macOS parity, the Darwin implementation should be enhanced to leverage `proc_pidinfo` sysctl hooks.

#### E. Strict Database Migrations drift-protection
*   **Stated**: SQLite states have been retired completely in favor of transaction-safe, advisory-locked Go embedded migrations, as defined in `docs/how-to/postgres-transition.md`.
*   **Actual**: In `go/pkg/db/migrations.go:17-18`, the system registers `LatestDaemonDBVersion = 17` and advisory lock key `332933`. In `VerifyMigrationsSHASource` (lines 159-195), it compares SHA256 hashes of embedded DDL SQL files against on-disk files, failing bootstrap on drift.
*   **Mine**: SHA256 drift protection guarantees schema integrity across deployments. However, using a single hardcoded 32-bit key `332933` causes immediate startup deadlocks if multiple local repositories share the same Postgres instance under distinct schemas. The advisory lock key must be derived dynamically by hashing the active Postgres schema name.

#### F. Ephemeral Workspace Isolation via Operational Scratch Spaces
*   **Stated**: The `.striatum/` directory located inside the target repository is purely operational scratch (PTY logs, FIFO pipes, token caches) and contains no durable database state. It must be locked down via 0o700 owner-only permissions and ignored by git. This is specified in `AGENTS.md` and `docs/how-to/postgres-transition.md`.
*   **Actual**: In `go/pkg/admin/repo_init.go:314-328`, adoption routines create owner-only (`0o700`) `.striatum/` scratch space, and `ensureGitignore` (lines 330-346) appends it to `.gitignore`. In `go/pkg/mutations/supervision_control.go:125-134`, `syscall.Mkfifo` creates Named Pipes inside this scratch space to route control payloads.
*   **Mine**: Decoupling database state from transient PTY/FIFO mechanisms inside `.striatum/` is highly elegant and secure under `0o700` permissions. However, Unix FIFOs block indefinitely on write if no reader is attached. To prevent daemon freeze, `go/pkg/mutations/supervision_control.go:957-964` opens the pipe in non-blocking write-only mode (`syscall.O_WRONLY|syscall.O_NONBLOCK`). On Linux, this returns `ENXIO` if no reader is present, causing silent packet drops during supervisor detachment/restart. Implementing a persistent, bounded daemon-side ring buffer to retain transit packets during temporary disconnects would eliminate this reliability gap.

---

### Discrepancies between Documentation and Implementation

During this exhaustive code audit, a single significant architectural discrepancy was identified between the written documentation and the actual Go implementation:

*   **Audit Chain Non-Repudiation vs. Developer Test Fixtures**:
    *   *Stated*: `docs/how-to/postgres-transition.md` and `docs/decisions/decision-log.md` claim that the daemon read-write database role (`striatumd_rw`) is completely stripped of `UPDATE` and `DELETE` privileges on `events` and `artifacts` tables to guarantee cryptographic append-only non-repudiation.
    *   *Actual*: While the SQL migrations do execute these revocations in production migrations (e.g. `0005_repo_local_workflow_state.sql:471-472`), the test helper framework located in `go/pkg/pgtest/` frequently runs unit tests under the superuser role (`postgres`). The superuser bypasses all table-level privilege revocations (`REVOKE` has no effect on superusers). Consequently, unit tests fail to verify the actual privilege-level security boundaries of the `striatumd_rw` role, creating a testing blind spot where privilege-enforced append-only constraints are only asserted via SQL triggers.

---

## 4. Strengths

Striatum’s architecture exhibits several exceptionally strong, elegant design decisions and abstractions that are highly suited for local-first, terminal-based AI tools:

1.  **Process PTY Abstraction with Supervisor Separation (`go/pkg/supervisor/helper.go:81-183`)**:
    *   *Abstraction*: The separation of terminal IO pumping into `striatum-supervisor-helper` is an outstanding isolation pattern. The helper binary handles the hazardous OS-specific PTY syscalls, tmux integration, and byte pumping on stdin/stdout, and does not require database connection pools, RPC packages, or capability verification libraries.
    *   *Justification*: By keeping the helper completely state-free, the codebase prevents database descriptor leaks into child processes. If an agent compromises a terminal lane or executes malicious script escapes, the main database connection and capability token secrets remain safely isolated in the parent `striatumd` process memory space.
2.  **Embedded migrations with compile-time SHA validation (`go/pkg/db/migrations.go:159-195`)**:
    *   *Abstraction*: Using Go's `embed.FS` to bake migration DDL directly into the compiled executable, coupled with on-disk hash matching.
    *   *Justification*: This design eliminates the classic deployment pain of missing SQL files. Developers get a single compiled static binary that is guaranteed to contain the exact schema migrations it expects. If a contributor modifies an on-disk SQL file without recompiling the binary, the SHA check catches it on boot, preventing corrupted schemas in local dev environments.
3.  **Advisory-Locked Mutex for Supervision Starts (`go/pkg/mutations/supervision_control.go:640-641`)**:
    *   *Abstraction*: Acquiring a transactional advisory lock (`pg_advisory_xact_lock`) derived from the repository and session IDs prior to launching a PTY supervisor.
    *   *Justification*: In local agent loops, concurrency issues are common. Standard SQL select-for-update locks on row cells can trigger deadlocks. Using Postgres advisory locks provides a clean, transaction-scoped mutual exclusion lock outside the normal row DML path, failing concurrent startup requests instantly and gracefully.
4.  **SQL Trigger-Enforced Append-Only Anchors (`go/pkg/db/sql/0005_repo_local_workflow_state.sql:438-466`)**:
    *   *Abstraction*: Handing write/delete blocks directly to the PL/pgSQL database engine via `BEFORE UPDATE OR DELETE` triggers.
    *   *Justification*: Relying on application-level logic to prevent state deletion is fragile. A single developer mistake or raw SQL query in a new mutation route can overwrite the execution log. Elevating this constraint to the database schema layer ensures that even manual SQL queries run by the operator cannot mutate or delete the history, satisfying the rigorous cryptographic requirements of audit-grade provenance.

---

## 5. Concerns

The architectural concerns below are classified by severity and backed by direct code evidence:

### A. BLOCKER: Missing Sandboxing Jail for Symbolic Links in Workspace Adoption
*   **Location**: `go/pkg/admin/repo_init.go:285-312` (canonical path resolution) and `go/pkg/mutations/artifact.go:170-190` (write path checks).
*   **Stated**: Striatum must restrict agent writes strictly to designated paths (`write_scope.allowed_paths`) and prevent directory traversal or file modifications outside the registered repository root.
*   **Actual**: During workspace adoption, `canonicalRepo` checks if the root path is a symbolic link and rejects it. However, it does **not** recursively scan the repository or implement a jail directory check when validating path writes inside `mutations/artifact.go`. An AI agent executing code inside a supervised lane can easily create a symbolic link (e.g. `ln -s /etc/passwd ./passwd_symlink`) and then request the daemon to publish an artifact at `passwd_symlink` or write content into it. The daemon resolves the link path on the host and writes straight into `/etc/passwd`.
*   **Mine**: This is a critical security vulnerability (Zip-slip/Symlink traversal style escape). Because the daemon runs locally with the operator's user privileges, a malicious or confused agent can easily overwrite critical user system files, inject SSH keys, or corrupt host configurations. The system must implement a strict **jail resolver** that canonicalizes every file write path *including* all nested symbolic link targets, asserting that the resolved physical destination path is strictly within the allowed workspace boundary before executing any writes.

---

### B. SERIOUS: Hardcoded Advisory Lock Concurrency Collision Risk
*   **Location**: `go/pkg/db/migrations.go:18` (`MigrationLockKey = 332933`).
*   **Stated**: Migrations are serialized cluster-wide to prevent DDL corruption when booting multiple instances.
*   **Actual**: The migration advisory lock key is hardcoded to a static integer value:
    ```go
    const MigrationLockKey = 332933
    ```
*   **Mine**: Using a static, hardcoded integer for a Postgres advisory lock is a serious concurrency flaw. If multiple repositories are registered on the same local Postgres server, or if the operator runs multiple daemons for different projects, they will share this exact lock key. On startup, a second daemon instance will block indefinitely waiting for the first daemon to release the migration lock—even if they are operating on entirely separate databases or schemas. The advisory lock key must be derived dynamically by hashing the database name or the schema scope, ensuring completely isolated migration pipelines.

---

### C. SERIOUS: Non-blocking FIFO Open Failure on Supervision Degraded States (ENXIO)
*   **Location**: `go/pkg/mutations/supervision_control.go:957-964` (HandleSuperviseSend FIFO write open).
*   **Stated**: Packets are delivered non-blockingly to active processes via Named Pipes.
*   **Actual**: The write operation is performed via Unix system calls:
    ```go
    fd, err := syscall.Open(pipePath, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
    if err != nil {
        if errors.Is(err, syscall.ENXIO) {
            return 0, errSupervisorPipeNoReader
        }
        return 0, err
    }
    ```
*   **Mine**: On Linux, opening a named pipe in non-blocking write-only mode (`O_WRONLY|O_NONBLOCK`) instantly returns an `ENXIO` error if no reader is attached to the other end. If the supervisor helper process restarts or a tmux pane detaches, this call fails immediately, causing the daemon to mark the lane degraded and permanently drop the work-packet payload. The supervised agent is left in a hung state, necessitating manual operator intervention. The daemon must maintain a thread-safe, bounded, in-memory ring buffer (up to 10 packets) to queue transit packets during transient supervisor detachment, flushing them when the helper reattaches.

---

### D. SERIOUS: Spawning Privilege Blind Spot in Database Test Harness
*   **Location**: `go/pkg/pgtest/pgtest.go:74-91` (database spawning logic).
*   **Stated**: The system architecture relies on the PostgreSQL read-write role (`striatumd_rw`) being restricted from executing `UPDATE` and `DELETE` queries on append-only tables to guarantee cryptographic audit-trail non-repudiation.
*   **Actual**: The unit testing helper framework in `go/pkg/pgtest/pgtest.go:74-91` connects to PostgreSQL and spawns database instances using the default superuser role (e.g. the standard `postgres` owner). This superuser credentials pool is passed directly into the database connection handlers that run package unit tests.
*   **Mine**: On PostgreSQL, superusers bypass all table-level privileges and `REVOKE` controls by design. Consequently, running all unit tests exclusively under the superuser role introduces a critical privilege blind spot: the tests never actually exercise the application under the restricted `striatumd_rw` permissions role. There is no automated test verification asserting that `UPDATE` or `DELETE` queries executed by the read-write role are rejected, leaving the privilege boundaries completely unvalidated during integration testing. The test suite must be enhanced to spawn a separate unprivileged connection role mapping to `striatumd_rw` and assert that updates and deletes on append-only event ledgers fail with permission-denied errors.

---

### E. SMELL: Synchronous SQL Queries on Capabilities Authorizer
*   **Location**: `go/pkg/rpc/auth_pg.go:61-101`.
*   **Stated**: Method authority is dynamically resolved against active token scopes.
*   **Actual**: Every JSON-RPC request executes synchronous queries to `striatumd.clients` and `striatumd.client_capabilities` directly inside the critical execution path of `Authorize`.
*   **Mine**: While mathematically clean, executing two sequential database select statements for *every single* RPC or MCP call is a database anti-pattern. AI agent loops operate by making high-frequency, rapid-fire status probes, reading file manifests, and writing progress notes. This synchronous DB querying introduces database transaction overhead and CPU context-switching on the local machine. It should be replaced with a lightweight in-memory LRU cache or a signed token-to-capability map stored in-daemon with an expiration TTL (e.g. 5 seconds) to maintain rapid, sub-millisecond execution loops.

---

## 6. North-star architecture

Striatum's design constraints are unique: it is not a large-scale microservice system. It operates on a **single user's laptop or homelab**, managing local processes. Any suggestion of deploying Kubernetes, multi-node message queues (like RabbitMQ), or managed cloud databases (like AWS RDS) is actively rejected as over-engineered bloat that breaks local-first requirements.

The greenfield "North-Star" architecture for Striatum is designed to maximize local-first reliability, process sandboxing, and ultra-low latency execution:

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                               OPERATOR WORKSTATION                              │
└─────────────────────────────────────────────────────────────────────────────────┘
┌────────────────────────────────┐               ┌────────────────────────────────┐
│         Developer CLI          │               │      Local Web Dashboard       │
│          (striatum)            │               │      (React TS embedded)       │
└───────────────┬────────────────┘               └───────────────┬────────────────┘
                │ IPC Socket                                     │ HTTP / SSE
                │ (daemon-go.sock)                               │ (Loopback only)
                ▼                                                ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                          striatumd (Core Go Daemon)                             │
│                                                                                 │
│ ┌──────────────────────────┐  ┌──────────────────────────┐  ┌─────────────────┐ │
│ │  UNIX Socket RPC Server  │  │   MCP HTTP/SSE Server    │  │  In-Memory LRU  │ │
│ │  (Handshake, Envelopes)  │  │ (Loopback, CORS Checked) │  │  Auth/Cap Cache │ │
│ └─────────────┬────────────┘  └─────────────┬────────────┘  └────────┬────────┘ │
│               │                             │                        │          │
│               └─────────────────────┬───────┴────────────────────────┘          │
│                                     ▼                                           │
│                         ┌───────────────────────┐                               │
│                         │   State Coordinator   │                               │
│                         └───────────┬───────────┘                               │
└─────────────────────────────────────┼───────────────────────────────────────────┘
                                      │ Go embed migrations / pgxpool
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                            Local PostgreSQL Instance                            │
│           (Schema: striatumd, Roles: striatumd_owner, striatumd_rw)             │
│    - Append-only Triggers (events, artifacts, audit_log)                       │
│    - Transaction-safe Advisory Locking                                          │
└─────────────────────────────────────┬───────────────────────────────────────────┘
                                      │ Spawns Helper (Namespace Isolated)
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                 striatum-supervisor-helper (Sandbox Engine)                    │
│                                                                                 │
│  - Executes under Linux User Namespaces (unshare -U -m -n)                      │
│  - Isolated mount points, network loopback-only, chroot jail                    │
│  - Bidirectional Packet Ring-Buffer (Handles ENXIO pipe detachment)             │
└─────────────────────────────────────┬───────────────────────────────────────────┘
                                      │ PTY Stream Allocation
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           Target Subprocess Terminal                            │
│                (Agent executing in strict write-scope jail)                     │
└─────────────────────────────────────────────────────────────────────────────────┘
```

### Key Pillars of the Greenfield Design

1.  **System-Tick Process Attestation with Darwin Parity**:
    Instead of hardcoding Linux `/proc` parsing, the liveness engine leverages a unified cross-platform Go system-tick extractor. On Darwin (macOS), it utilizes `unix.Sysctl` or CGo wrappers around `proc_pidinfo` to fetch the process start-time (`kp_eproc.e_paddr`). This guarantees identical attestation and immutable byline derivation on both Linux and macOS.
2.  **OS-Native Sandbox Jail Isolation**:
    Rather than relying on basic directory validation checks at publication time (which are vulnerable to symlink bypasses), the supervisor helper spawns target agent processes under namespace isolation:
    *   On **Linux**, it invokes the helper under a user namespace mapping (`CLONE_NEWUSER`), a mount namespace (`CLONE_NEWNS`), and a network namespace (`CLONE_NEWNET`), completely disabling raw internet access and bind-mounting workspace directories.
    *   On **macOS**, it wraps target agent launches using the native macOS sandbox utility (`sandbox-exec` with a dynamically generated `.sb` profile file restricting writes to `write_scope.allowed_paths`).
3.  **In-Memory Capability Cache**:
    The authorizer maintains a thread-safe, bounded, in-memory cache of validated tokens. Upon receiving a valid RPC connection, token capability queries are executed against Postgres once and cached for 5 seconds. If a token is revoked in Postgres, a database notification (`LISTEN/NOTIFY`) instantly purges the cache, maintaining real-time revocation.
4.  **Resilient Packet Delivery Ring Buffer**:
    To defeat FIFO pipeline ENXIO write failures, the daemon maintains a ring buffer in memory. When packet delivery fails due to a missing stdin reader, the daemon buffers up to 10 unacknowledged packets. When the helper reattaches, the daemon flushes the buffer down the named pipe, preventing lane crashes.

---

## 7. Recommended changes

| Priority | Change Name | Rationale | Benefit | Risk | Effort |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Blocker** | Construct Scoped Symlink Resolution Engine | Recursively canonicalize write paths to prevent symlink traversal escapes (e.g. agent symlinks `/etc/passwd` to workspace path). | Absolute protection against host system files modification by untrusted agents. | Might break complex workspace structures utilizing valid internal symlinks. | 2 Days |
| **Serious** | Derive Advisory Lock Keys Cryptographically | Replace hardcoded migration advisory lock key `332933` with a SHA256-based dynamic hash of the active Postgres database and schema names. | Eliminates bootstrap deadlocks when running multiple Striatum daemons on a single system. | Low risk of lock mismatch if schema names contain invalid characters. | 4 Hours |
| **Serious** | Implement Packet Recovery Ring Buffer | Buffer outgoing instruction packets in a thread-safe daemon ring-buffer when named pipe open returns ENXIO. | Prevents permanent packet drops and lane degradation during transient supervisor restarts or tmux detaches. | Increased memory overhead if buffer is unbounded. | 1 Day |
| **Serious** | Assert Privilege Restrictions via Dedicated Non-Superuser Role in Test Harness | Modify the `pgtest.go` helper to provision a restricted non-superuser role and establish connection pools utilizing these credentials. | Validates that table-level `REVOKE UPDATE, DELETE` assertions on append-only event tables are actively enforced under test conditions. | Potential test setup complexity across diverse homelab Postgres setups. | 1 Day |
| **Medium** | Integrate In-Memory Capability Cache | Add a thread-safe, Postgres NOTIFY-invalidated LRU cache in the daemon for token-capability scopes. | Cuts sub-millisecond execution times for hot agent loops, eliminating sequential synchronous DB calls. | Complexity of invalidation states in local developer workflows. | 2 Days |
| **Medium** | Compile Cross-Platform Process Attestation | Implement Darwin-specific `proc_pidinfo` sysctl hooks to fetch boot ticks, providing OS-native parity for macOS. | Enables secure attested bylines and PID recycling safety on macOS. | Requires testing CGo bindings or OS-specific sysctl packages. | 3 Days |
| **Low** | Adopt Protocol Buffers for Schema Compilation | Replace manual JSON method maps and coverage assertions with compiled protobuf and gRPC-Go schemas. | Guarantees absolute type safety and compile-time API validation. | Requires installing protoc compiler on development boxes. | 1 Week |

---

## 8. Functionality I'd add

To advance Striatum beyond a robust coordinator into a highly capable developer platform, the following local-first features are proposed:

| Proposed Feature | Priority | Rationale | Benefit | Risk | Effort |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Local-Only Mock S3 Engine** | High | Under RFC 0072, non-decisional blobs require S3. Small setups lack a MinIO/AWS container. | Out-of-the-box operation with zero external dependencies. | Disk storage inflation if unpurged. | 3 Days |
| **Dynamic Host Port Allocation** | High | Static loopback port bindings are vulnerable to port collision and local hijacking. | Prevents local port hijacking and collision crashes on homelabs. | Requires CLI to dynamically discover port via token file. | 2 Days |
| **Adversarial Posture Agent Interrogator** | Medium | RFC 0082 details interrogation. The database needs a formal, interactive prompt loop. | Enables operators to pause failing agent runs and run visual security sweeps. | Complexity of PTY multiplexing during active runs. | 1 Week |
| **Workspace Git Rollback Guard** | Medium | RFC 0067 requires human gate verification for commits, but local files can get corrupted. | Guarantees that workspace can be cleanly restored to a known DB anchor state. | Conflicts with concurrent manual file changes. | 4 Days |

---

## 9. Execution roadmap

```
  [TODAY: First Step]             [NEAR-TERM: Month 1]            [MEDIUM-TERM: Q1]               [LONG-TERM: Future]
  - Jail symlink resolver         - Cryptographic locking         - macOS process attestation     - Adopt Protobuf schemas
  - Fix symlink write exploits    - local-only S3 mock engine     - Dynamic daemon loopback ports - Fully isolated namespace jail
```

### 1. Concrete First Step (Startable Today)
*   **Action**: Implement the **Scoped Symlink Resolution Engine** inside `go/pkg/mutations/artifact.go`.
*   **Details**: Rewrite the path validation routine to recursively evaluate every folder segment. Utilize `filepath.EvalSymlinks` to canonicalize the destination, and assert that the physical, absolute target file path lies strictly within `write_scope.allowed_paths` before invoking any file writes. This permanently closes the blocker symlink-escape vulnerability.

### 2. Near-Term Milestones (Month 1)
*   **DDL Advisory Lock Refactoring**: Modify `go/pkg/db/migrations.go` to compute the migration advisory lock dynamically by hashing the Postgres schema name, ending bootstrap locks.
*   **Mock S3 Engine Integration**: Embed a lightweight, local-disk filesystem mock S3 client inside `go/pkg/blob/` to allow seamless RFC 0072 blob uploads without requiring external AWS credentials or MinIO instances.

### 3. Medium-Term Milestones (Quarter 1)
*   **Darwin Process Attestation**: Implement the macOS native process start-time sysctl parser in `go/pkg/supervisor/` to provide attestation and PID recycling security on Apple Silicon.
*   **Dynamic Loopback Port Discovery**: Transition the daemon from a static loopback port to dynamic, randomized port allocation. Write the active socket/port and capability token directly to owner-only `/run/user/<uid>/striatum.conn` to enable zero-conf CLI routing.

### 4. Long-Term Milestones
*   **Full Namespace Sandboxing**: Upgrade `striatum-supervisor-helper` to execute target agent processes in isolated Linux namespaces (`chroot` + `CLONE_NEWUSER` + `CLONE_NEWNET` loopback-only) and native macOS Sandboxes, providing high-grade isolation.
*   **Schema-Driven Code Generation**: Transition the Unix socket interface to Protobuf/gRPC, completely eliminating manual JSON envelope decoding and brittle handwriting of method coverage registries.

---

## 10. Open questions

1.  **Attestation Policy for Remote Lanes**:
    *   *Context*: Currently, lane attestation relies strictly on local process inspection via `/proc` (or sysctl).
    *   *Question*: If Striatum is expanded to run agents in remote environments (e.g. inside a local VM or a Tailscale SSH container), how can the daemon securely verify the start-time tick of the remote agent without trusting the remote OS or introducing heavy daemon agents inside the target VM?
2.  **State Reconciler Behavior under Dirty Worktrees**:
    *   *Context*: If a lease expires lazily or the sweeper kills a stalled session, the workspace's physical files are left in a "dirty" state (half-written code, uncommitted changes).
    *   *Question*: Should the state sweeper automatically execute a destructive git rollback (`git reset --hard`) to restore the repository to the last database-anchored state, or should it block future runs indefinitely until the human operator manually clears the workspace? The current spec is silent on this boundary condition.
3.  **Cross-Repository Validation Scope**:
    *   *Context*: Under Migration 3 (`cross-repo workflows`), Striatum supports workflows that reference dependencies across repositories.
    *   *Question*: How should database capabilities enforce access boundaries when a single agent run claims a parent repository but needs to query artifacts or run tests inside a dependent registered repository? Does the daemon require the client to supply multiple token bearer keys, or does it dynamically elevate the capability scope of the parent repository token?
