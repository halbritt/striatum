package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/lanehealth"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	"github.com/jackc/pgx/v5"
)

func HandleClaimNext(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	if sessionID == "" {
		return nil, rpc.NewError("schema_invalid", "work.claim_next requires session_id", nil)
	}
	// RFC 0096 V2 / #135: a session-bound capability token may only claim work as
	// its own session; an unbound operator/coordinator token may claim for any
	// session (override). Closes cross-session claim spoofing now that live lanes
	// carry their bound token.
	if _, err := enforceSessionBinding(ctx, sessionID); err != nil {
		return nil, err
	}
	leaseSeconds := intParam(envelope, "lease_seconds", 3600)
	if leaseSeconds <= 0 {
		leaseSeconds = 3600
	}

	// #103: sibling reviewers claiming in parallel (work.await_packet -> claim)
	// can have Postgres abort one transition with a deadlock (SQLSTATE 40P01).
	// The claim is idempotent (it re-selects a claimable message on retry), so
	// retry the whole transaction a bounded number of times rather than surfacing
	// a transient control-plane error to the lane — matching #98's review.submit
	// treatment, here for the claim/await path.
	return withTxRetryOnDeadlock(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		return claimNextInTx(ctx, tx, repositoryID, sessionID, leaseSeconds)
	})
}

func claimNextInTx(ctx context.Context, tx db.TxRunner, repositoryID, sessionID string, leaseSeconds int) (map[string]any, error) {
	// RFC 0104 per-run serialization invariant: take the per-run advisory lock
	// as the FIRST statement, before the `sessions`/`runs` FOR UPDATE rows
	// below, so the claim path shares an identical, earlier serialization point
	// with the verdict-completion and recovery-sweep paths and the
	// {sessions, runs} deadlock cycle cannot form. This generalizes the
	// RFC 0101 Phase 0a interrogation-only lock to every claim.
	if err := lockRunForSession(ctx, tx, repositoryID, sessionID); err != nil {
		return nil, err
	}
	session, err := rowByID(ctx, tx, repositoryID, "sessions", "session_id", sessionID, true)
	if err != nil {
		return nil, err
	}
	// RFC 0095 §4 (F-I/#81): a closed/expired/stopped/lost session must never
	// be granted work. A session closed with close_reason
	// interrogation_window_closed whose supervised process is still alive used
	// to reclaim its revision-cycle job here, letting a prior author rewrite
	// its own challenged work without the fresh context the gate required.
	// Refuse any non-active session up front.
	if state := fmt.Sprint(session["state"]); state != "active" {
		return nil, rpc.NewError("invalid_transition", fmt.Sprintf("session is %s; register a fresh session", state), nil)
	}
	runID := fmt.Sprint(session["run_id"])
	run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true)
	if err != nil {
		return nil, err
	}
	if _, err := expireLeases(ctx, tx, repositoryID, runID); err != nil {
		return nil, err
	}
	state := fmt.Sprint(run["state"])
	if state == "needs_branch_confirmation" || state == "ready" {
		return nil, rpc.NewError("branch_confirmation_required", "branch confirmation and run start are required before claims", nil)
	}
	if state != "running" {
		// #207 (part A): a bare no_work when the run is needs_operator is an
		// invisible gate — a fresh, eligible session polls a pending, immediately
		// visible message and gets no_work forever, with no signal that the run is
		// frozen pending an operator. Surface the reason (mirroring the #107
		// fresh_session ineligibility pattern) and, when cheaply queryable, the open
		// escalation/blocker id so the operator knows exactly what to resolve.
		if state == "needs_operator" {
			res := map[string]any{
				"status":            "no_work",
				"ineligible_reason": "run_needs_operator",
				"hint":              "the run is frozen pending an operator; resolve the open escalation (striatum escalation resolve <id>) or otherwise return the run to running before this session can claim work.",
			}
			if blockerID := openEscalationBlockerForRun(ctx, tx, repositoryID, runID); blockerID != "" {
				res["blocker_id"] = blockerID
			}
			return res, nil
		}
		return map[string]any{"status": "no_work"}, nil
	}
	if nullable(run["paused_at"]) != nil {
		return map[string]any{"status": "no_work", "paused": true}, nil
	}
	if err := ensureWorkSessionBackend(ctx, tx, repositoryID, sessionID, "claim"); err != nil {
		return nil, err
	}
	rows, err := queryRows(ctx, tx, `
			SELECT qm.*
			  FROM striatumd.queue_messages qm
			  JOIN striatumd.jobs j
			    ON j.repository_id = qm.repository_id
			   AND j.job_id = qm.job_id
			 WHERE qm.repository_id = $1
			   AND qm.kind = 'work'
			   AND qm.state = 'pending'
			   AND qm.target_role_id = $2
			   AND (qm.target_lane_id IS NULL OR qm.target_lane_id = $3)
			   AND (
			     j.fresh_session_required = false
			     OR NOT EXISTS (
			       SELECT 1 FROM striatumd.work_packets wp
			        WHERE wp.repository_id = qm.repository_id
			          AND wp.run_id = qm.run_id
			          AND wp.session_id = $4
			          AND wp.job_id != qm.job_id
			     )
			   )
			   AND qm.run_id = $5
			 ORDER BY qm.priority DESC, qm.created_at ASC
			 LIMIT 1
			 FOR UPDATE OF qm SKIP LOCKED`,
		repositoryID,
		session["role_id"],
		session["lane_id"],
		sessionID,
		runID,
	)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		// #107: distinguish "no work at all" from "work exists but this session
		// is structurally ineligible" — most commonly a fresh_session_required
		// job for this role that this (already-spent) session can never claim.
		// Surfacing the reason lets the lane stop polling and the coordinator
		// register a fresh session instead of inferring the mismatch.
		if wfJob := freshSessionBlockedWorkflowJob(ctx, tx, repositoryID, fmt.Sprint(session["role_id"]), fmt.Sprint(session["lane_id"]), sessionID, runID); wfJob != "" {
			return map[string]any{
				"status":            "no_work",
				"ineligible_reason": "fresh_session_required",
				"workflow_job_id":   wfJob,
				"hint":              "a queued job for this role requires a fresh session; stop this lane and register/start a fresh session to claim it (striatum register-session <run> <role> <lane> --fresh).",
			}, nil
		}
		return map[string]any{"status": "no_work"}, nil
	}
	chosen := rows[0]
	jobID := fmt.Sprint(chosen["job_id"])
	job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		return nil, err
	}
	// #222: fresh-review PROCESS-lineage gate. The fresh-session gate above only
	// guarantees a new session id; a long-lived lane process can register a fresh
	// session and review its own upstream work. Refuse a fresh-context review job
	// when the claimant's supervised process did durable upstream work (or its
	// process identity is unverifiable), leaving the job queued for a real fresh
	// reviewer. Runs before any lease/packet is created.
	if refusal, err := freshReviewProcessLineageRefusal(ctx, tx, repositoryID, runID, sessionID, job); err != nil {
		return nil, err
	} else if refusal != nil {
		return refusal, nil
	}
	return claimChosenJob(ctx, tx, repositoryID, sessionID, runID, session, run, job, chosen, leaseSeconds)
}

// claimChosenJob performs the actual claim of a selected pending job for a
// session: the lease + queue-message + job state transitions, the packet render
// and durable work_packets persistence, liveness, and the queue.claimed event.
// It is the shared core of work.claim_next (after its eligibility gates) and the
// admin work.claim_override path (#222), so both produce an identical, auditable
// claim.
func claimChosenJob(ctx context.Context, tx db.TxRunner, repositoryID, sessionID, runID string, session, run, job, chosen map[string]any, leaseSeconds int) (map[string]any, error) {
	jobID := fmt.Sprint(job["job_id"])
	now := nowString()
	leaseID, err := newID("lease")
	if err != nil {
		return nil, err
	}
	packetID, err := newID("wp")
	if err != nil {
		return nil, err
	}
	expiresAt := expiresAfter(leaseSeconds)
	if err := tx.Exec(ctx, `
			INSERT INTO striatumd.leases (
			  repository_id, lease_id, run_id, resource_type, resource_id,
			  owner_session_id, state, acquired_at, expires_at, last_heartbeat_at
			)
			VALUES ($1,$2,$3,'job',$4,$5,'active',$6,$7,$8)`,
		repositoryID, leaseID, runID, jobID, sessionID, now, expiresAt, now); err != nil {
		return nil, err
	}
	if err := tx.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'claimed', claimed_at = $1, updated_at = $2,
			       current_lease_id = $3, claim_count = claim_count + 1
			 WHERE repository_id = $4 AND message_id = $5`,
		now, now, leaseID, repositoryID, chosen["message_id"]); err != nil {
		return nil, err
	}
	if err := tx.Exec(ctx, `
			UPDATE striatumd.jobs
			   SET state = 'claimed', current_lease_id = $1, started_at = $2
			 WHERE repository_id = $3 AND job_id = $4`,
		leaseID, now, repositoryID, jobID); err != nil {
		return nil, err
	}
	packet, err := buildPacket(ctx, tx, repositoryID, run, session, job, fmt.Sprint(chosen["message_id"]), leaseID, expiresAt, packetID)
	if err != nil {
		return nil, err
	}
	packetJSON, err := json.Marshal(packet)
	if err != nil {
		return nil, err
	}
	packetSum := sha256.Sum256(packetJSON)
	packetJSONArg, err := db.JSONBArg(tx, packet)
	if err != nil {
		return nil, err
	}
	if err := tx.Exec(ctx, `
			INSERT INTO striatumd.work_packets (
			  repository_id, packet_id, run_id, job_id, message_id, lease_id,
			  session_id, packet_json, packet_sha256, created_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10)`,
		repositoryID,
		packetID,
		runID,
		jobID,
		chosen["message_id"],
		leaseID,
		sessionID,
		packetJSONArg,
		hex.EncodeToString(packetSum[:]),
		now,
	); err != nil {
		return nil, err
	}
	if err := sessionliveness.Record(ctx, tx, repositoryID, sessionID, sessionliveness.LastPacketDeliveredAt); err != nil {
		return nil, err
	}
	if _, err := appendEvent(ctx, tx, repositoryID, runID, "queue.claimed", sessionID, jobID, chosen["message_id"], nil, leaseID, nil); err != nil {
		return nil, err
	}
	// #68: when a self-driving supervisor (agent_loop_mode = self_driving) is
	// attached, the agent itself drives delivery via work.await_packet. An
	// operator-issued `supervise send` against that session double-claims and
	// deadlocks, so suppress the supervise_send hint and tell the operator the
	// agent self-claims instead.
	selfDriving, err := sessionHasSelfDrivingSupervisor(ctx, tx, repositoryID, sessionID)
	if err != nil {
		return nil, err
	}
	return claimNextResult(sessionID, packetID, packet, selfDriving), nil
}

// sessionBackendGateError signals that the session backend gate refused work and
// that a terminal session/supervisor transition must be COMMITTED as cleanup.
// The transition cannot run inside the work tx — that tx returns this error and
// rolls back, undoing the write — nor in a concurrent side tx, because the work
// tx holds the session row FOR UPDATE. withTx therefore rolls the work tx back
// first, then runs cleanup on a fresh pooled connection and surfaces rpcErr.
type sessionBackendGateError struct {
	rpcErr  *rpc.Error
	cleanup func(ctx context.Context, runner db.Runner) error
}

func (e *sessionBackendGateError) Error() string { return e.rpcErr.Error() }

func (e *sessionBackendGateError) Unwrap() error { return e.rpcErr }

// missingBackendGateError refuses work and defers expiring the active session
// (no attached supervisor / no readable backend) until the work tx rolls back.
func missingBackendGateError(repositoryID, sessionID, phase, reason string) *sessionBackendGateError {
	return &sessionBackendGateError{
		rpcErr: rpc.NewError("invalid_transition", "session cannot "+phase+" work without an attached supervisor; start or register a fresh session", nil),
		cleanup: func(ctx context.Context, runner db.Runner) error {
			return markActiveSessionTerminal(ctx, runner, activeSessionTerminalUpdate{
				RepositoryID: repositoryID,
				SessionID:    sessionID,
				State:        "expired",
				Reason:       phase + " refused: " + reason,
			})
		},
	}
}

func ensureWorkSessionBackend(ctx context.Context, tx db.TxRunner, repositoryID, sessionID, phase string) error {
	supervisor, err := requireActiveControlSupervisor(ctx, tx, repositoryID, sessionID, true)
	if err != nil {
		var rpcErr *rpc.Error
		if errors.As(err, &rpcErr) && rpcErr.Code == "invalid_transition" {
			return missingBackendGateError(repositoryID, sessionID, phase, "no_attached_supervisor")
		}
		return err
	}
	if supervisor.State != "attached" {
		return rpc.NewError("invalid_transition", fmt.Sprintf("session cannot %s work because supervisor %s is %s; rebridge, stop, or register a fresh session", phase, supervisor.SupervisorID, supervisor.State), nil)
	}
	checker := lanehealth.Checker{
		Probe: lanehealth.ProdProbe{Runner: supervisionTmuxRunner},
	}
	health, err := checker.Check(ctx, tx, repositoryID, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return missingBackendGateError(repositoryID, sessionID, phase, "no_attached_supervisor")
		}
		return err
	}
	if health.LiveTarget() {
		return nil
	}
	// A backend that is ALIVE but only attention-pending — a pending interrogation
	// question or escalation — is a live attested lane waiting on a human/
	// interrogator, not a dead backend. Admit it; a genuinely dead lane fails the
	// Alive probe and is still refused below. Only death/idle stalls
	// (discovery/await/ack/lease-heartbeat/protocol-idle) and missing backends are
	// refused.
	if health.Alive && isAttentionPendingStall(health.Stall.StallClass) {
		return nil
	}
	reason := string(health.Reason)
	if health.Stall.StallClass != "" {
		reason = health.Stall.StallClass
	}
	if reason == "" {
		reason = "unattested_lane_backend"
	}
	refusal := rpc.NewError("invalid_transition", "session cannot "+phase+" work without a live attested lane backend: "+reason, nil)
	if backendLossShouldMarkSessionLost(health.Reason) {
		sup := supervisor
		lostReason := phase + " refused: " + reason
		lostPayload := map[string]any{
			"phase":               "work." + phase,
			"lane_attestation":    "unattested",
			"attestation_reason":  reason,
			"supervisor_liveness": health.LivenessClass,
		}
		return &sessionBackendGateError{
			rpcErr: refusal,
			cleanup: func(ctx context.Context, runner db.Runner) error {
				return markSupervisorLost(ctx, runner, repositoryID, sup.SupervisorID, sup.RunID, sup.SessionID, lostReason, sup.PID, lostPayload)
			},
		}
	}
	return refusal
}

// isAttentionPendingStall reports whether a stall class means the lane is alive
// and waiting on a human/interrogator (a pending interrogation question or
// escalation) rather than dead or idle. Such a lane has a live attested backend,
// so the work-session backend gate must admit it.
func isAttentionPendingStall(stallClass string) bool {
	return stallClass == sessionliveness.StallQuestionPending ||
		stallClass == sessionliveness.StallEscalationPending
}

func backendLossShouldMarkSessionLost(reason lanehealth.LaneReason) bool {
	switch reason {
	case lanehealth.ReasonPIDMissing, lanehealth.ReasonPIDGone, lanehealth.ReasonPIDIdentityMismatch:
		return true
	default:
		return false
	}
}

func claimNextResult(sessionID, packetID string, packet map[string]any, selfDrivingSupervisor bool) map[string]any {
	result := map[string]any{
		"status":    "claimed",
		"packet_id": packetID,
		"packet":    packet,
	}
	if selfDrivingSupervisor {
		// The agent's own await loop will deliver this packet; a manual
		// supervise_send would double-claim. Surface the self-claim note instead
		// of the misleading supervise_send command.
		result["next_steps"] = map[string]any{
			"self_claim_note": "session has an attached self-driving supervisor; the agent self-claims via work.await_packet — do not run `supervise send`",
		}
		return result
	}
	result["next_steps"] = map[string]any{
		"supervise_send": "striatum supervise send --session-id " + sessionID + " --packet-id " + packetID,
	}
	return result
}

// sessionHasSelfDrivingSupervisor reports whether the session has an active
// (starting/attached/detached) supervisor running in agent_loop self_driving
// mode. The agent_loop_mode is recorded in the supervisor pointer's
// metadata_json at supervise-start time; a self-driving agent calls
// work.await_packet itself, so the claim-path must not emit a supervise_send
// hint (#68).
func sessionHasSelfDrivingSupervisor(ctx context.Context, runner any, repositoryID, sessionID string) (bool, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT COALESCE(p.metadata_json, '{}'::jsonb) AS metadata_json
		  FROM striatumd.process_supervisors ps
		  LEFT JOIN striatumd.process_supervisor_pointers p
		    ON p.repository_id = ps.repository_id AND p.supervisor_id = ps.supervisor_id
		 WHERE ps.repository_id = $1 AND ps.session_id = $2
		   AND ps.state = ANY($3)
		 ORDER BY ps.started_at DESC, ps.supervisor_id DESC
		 LIMIT 1`,
		repositoryID, sessionID, []string{"starting", "attached", "detached"})
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	metadata := asMap(rows[0]["metadata_json"])
	mode, _ := metadata["agent_loop_mode"].(string)
	return mode == agentLoopModeSelfDriving, nil
}

func buildPacket(
	ctx context.Context,
	runner any,
	repositoryID string,
	run map[string]any,
	session map[string]any,
	job map[string]any,
	messageID string,
	leaseID string,
	leaseExpiresAt string,
	packetID string,
) (map[string]any, error) {
	snapshot, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(run["workflow_snapshot_id"]), false)
	if err != nil {
		return nil, err
	}
	workflow := asMap(snapshot["workflow_json"])
	roles := asMap(workflow["roles"])
	roleDef := asMap(roles[fmt.Sprint(job["role_id"])])
	writeScope := asMap(job["write_scope_json"])
	if frozenScope, _, ok := frozenAttemptWriteScope(ctx, runner, repositoryID, job, ""); ok && len(frozenScope) > 0 {
		writeScope = frozenScope
		job["write_scope_json"] = frozenScope
	}
	// Build finding 1: resolve cycle-scoped expected artifacts (e.g. the
	// adjudicator's collaboration_ledger) against the job attempt so the agent is
	// told the concrete cycle_<attempt> logical name + path to publish to.
	expectedArtifacts := resolveExpectedArtifactCycles(asList(job["expected_artifacts_json"]), intValue(job["attempt"]))
	laneSelector := asMap(job["lane_selector_json"])
	laneID, _ := laneSelector["lane_id"].(string)
	if laneID == "" {
		laneID = fmt.Sprint(session["lane_id"])
	}
	lanes := asMap(workflow["lanes"])
	laneConfig := asMap(lanes[laneID])
	worktreeIsolation := laneWorktreeIsolation(workflow, laneID)
	worktreeRequired := worktreeIsolation == "per_job" && isRepoWrite(job)
	attestation := sessionLaneAttestation(ctx, runner, repositoryID, fmt.Sprint(session["session_id"]))
	attested, _ := attestation["attested"].(bool)
	operatorLabel, _ := nullable(session["operator_label"]).(string)
	author := artifactAuthorIdentity(
		workflow,
		fmt.Sprint(job["role_id"]),
		laneID,
		fmt.Sprint(job["workflow_job_id"]),
		intValue(session["ordinal"]),
		attested,
		operatorLabel,
	)
	authorLine, _ := author["line"].(string)
	if authorLine == "" {
		return nil, rpc.NewError("invalid_transition", "session author line could not be derived", nil)
	}
	requirements := asMap(job["capability_requirements_json"])
	// RFC 0082 §6: relax fresh_session_required for an interrogable builder so
	// its context survives into the interrogation/review window.
	interrogable := workflowJobInterrogable(workflow, fmt.Sprint(job["workflow_job_id"]))
	freshRequired := boolValue(job["fresh_session_required"]) && !interrogable
	packetContext := map[string]any{
		"docs":         asList(workflow["context_docs"]),
		"content_mode": "references",
	}
	if envelope := downstreamImplementationEnvelopeForPacket(workflow, fmt.Sprint(job["workflow_job_id"])); envelope != nil {
		packetContext["implementation_envelope"] = envelope
	}
	if resources := sharedResourcesForPacket(workflow, fmt.Sprint(job["workflow_job_id"])); resources != nil {
		packetContext["shared_resources"] = resources
	}
	if augmentation := augmentationReferences(workflow, fmt.Sprint(job["workflow_job_id"]), fmt.Sprint(run["repo_root"])); augmentation != nil {
		packetContext["augmentation_references"] = augmentation
	}
	targets, err := interrogationTargetsForPacket(ctx, runner, repositoryID, fmt.Sprint(run["run_id"]), workflow, fmt.Sprint(job["workflow_job_id"]))
	if err != nil {
		return nil, err
	}
	if len(targets) > 0 {
		packetContext["interrogation_targets"] = targets
	}
	// RFC 0095 Goal 8 / #206: a re-opened review/verdict-capable job (attempt > 1)
	// carries the prior round's finding + verdict so the lane reviews the CURRENT
	// revision instead of republishing the stale finding it may find on disk.
	if revision, rerr := revisionContextForPacket(ctx, runner, repositoryID, job); rerr != nil {
		return nil, rerr
	} else if revision != nil {
		packetContext["revision_context"] = revision
	}
	// #105: workflow-declared paths (role definition, context docs, prompts) are
	// relative to the workflow directory, but a lane runs from the repo root.
	// Surface the repo-root-relative workflow_root as the explicit base, and
	// resolve the role definition_path to a repo-root-relative path so it opens
	// on the first try (keeping the workflow-relative form for reference).
	workflowRoot := workflowRootDir(snapshot)
	roleBlock := map[string]any{
		"role_id":         job["role_id"],
		"definition_path": roleDef["definition_path"],
		"inline_summary":  roleDef["summary"],
	}
	if rawDef, _ := roleDef["definition_path"].(string); rawDef != "" {
		if resolved, rel := resolveWorkflowRelativePath(rawDef, workflowRoot); resolved != "" {
			roleBlock["definition_path"] = resolved
			if rel != "" {
				roleBlock["workflow_relative_path"] = rel
			}
		}
	}
	packet := map[string]any{
		"packet_version": "striatum.work-packet.v1",
		"packet_id":      packetID,
		"run": map[string]any{
			"run_id":        run["run_id"],
			"workflow_id":   workflow["workflow_id"],
			"repo_root":     run["repo_root"],
			"workflow_root": workflowRoot,
			"branch": map[string]any{
				"name":      run["branch_name"],
				"confirmed": nullable(run["branch_confirmed_at"]) != nil,
			},
		},
		"session": map[string]any{
			"session_id":   session["session_id"],
			"slug":         session["slug"],
			"role_id":      session["role_id"],
			"lane_id":      session["lane_id"],
			"capabilities": asList(session["capabilities_json"]),
		},
		"lane_attestation": attestation,
		"lease": map[string]any{
			"lease_id":   leaseID,
			"message_id": messageID,
			"expires_at": leaseExpiresAt,
			// #174: the advertised heartbeat cadence must be the actual window the
			// liveness sweep keys on (DefaultPolicy().LeaseHeartbeatSeconds), not a
			// magic 300 literal decoupled from the lease TTL. An agent that
			// heartbeats within this window keeps its lease classified live; the
			// far-future expires_at is the hard lease TTL, not the heartbeat floor.
			"heartbeat_after_seconds": sessionliveness.DefaultPolicy().LeaseHeartbeatSeconds,
		},
		"job": map[string]any{
			"job_id":                 job["job_id"],
			"workflow_job_id":        job["workflow_job_id"],
			"attempt":                job["attempt"],
			"type":                   job["job_type"],
			"title":                  job["title"],
			"author":                 author,
			"objective":              requirements["objective"],
			"fresh_session_required": freshRequired,
			"interrogable":           interrogable,
		},
		"role":                roleBlock,
		"context":             packetContext,
		"task_prompt":         packetTaskPrompt(asMap(requirements["task_prompt"]), snapshot),
		"inputs":              asList(requirements["inputs"]),
		"write_scope":         writeScope,
		"adapter_constraints": buildAdapterConstraints(laneConfig),
		"expected_artifacts":  expectedArtifactsWithAuthor(expectedArtifacts, authorLine),
		"worktree_isolation":  worktreeIsolation,
		"worktree_required":   worktreeRequired,
		"commands":            buildPacketCommands(fmt.Sprint(session["session_id"]), fmt.Sprint(job["job_id"]), messageID, leaseID, worktreeRequired),
		"artifact_policy": map[string]any{
			"publish_transcripts":    false,
			"curated_artifacts_only": true,
		},
	}
	if baseline := buildWriteScopeBaseline(ctx, fmt.Sprint(run["repo_root"]), writeScope); baseline != nil {
		packet["write_scope_baseline"] = baseline
	}
	if policy := buildReviewPolicy(workflow, fmt.Sprint(job["workflow_job_id"])); policy != nil {
		packet["review_policy"] = policy
	}
	if profile := harnessProfileView(workflow, laneID); profile != nil {
		packet["harness_profile"] = profile
	}
	return packet, nil
}

func buildWriteScopeBaseline(ctx context.Context, repoRoot string, writeScope map[string]any) map[string]any {
	if !isRepoWriteScope(writeScope) {
		return nil
	}
	allowed := stringListFromAny(writeScope["allowed_paths"])
	forbidden := stringListFromAny(writeScope["forbidden_paths"])
	if len(allowed) == 0 && len(forbidden) == 0 {
		return nil
	}
	changed, err := gitChangedPathSnapshots(ctx, repoRoot)
	if err != nil {
		return map[string]any{"status": "unavailable", "error": err.Error()}
	}
	entries := make([]map[string]any, 0, len(changed))
	for _, item := range changed {
		entries = append(entries, map[string]any{"path": item.Path, "hash": item.Hash})
	}
	return map[string]any{"status": "captured", "changed_paths": entries}
}

func packetTaskPrompt(taskPrompt map[string]any, snapshot map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range taskPrompt {
		result[key] = value
	}
	rawPath, _ := result["path"].(string)
	if rawPath == "" || strings.HasPrefix(rawPath, "/") {
		return result
	}
	sourcePath, _ := snapshot["source_path"].(string)
	if sourcePath == "" {
		return result
	}
	sourceDir := path.Dir(sourcePath)
	if sourceDir == "." || sourceDir == "/" {
		return result
	}
	cleanedRaw := path.Clean(rawPath)
	cleanSourceDir := path.Clean(sourceDir)
	// #90: when the prompt path is ALREADY repo-relative (already prefixed with
	// the workflow directory), joining the workflow dir again duplicates it
	// (striatum/<wf>/striatum/<wf>/prompts/x.md). Treat the path as already
	// resolved and derive the workflow-relative form by stripping the prefix.
	if cleanedRaw == cleanSourceDir || strings.HasPrefix(cleanedRaw, cleanSourceDir+"/") {
		workflowRelative := strings.TrimPrefix(cleanedRaw, cleanSourceDir+"/")
		result["path"] = cleanedRaw
		if workflowRelative != "" && workflowRelative != cleanedRaw {
			result["workflow_relative_path"] = workflowRelative
			result["workflow_source_path"] = sourcePath
		}
		return result
	}
	resolved := path.Clean(path.Join(sourceDir, rawPath))
	if resolved == "." || resolved == ".." || strings.HasPrefix(resolved, "../") {
		return result
	}
	if resolved != cleanedRaw {
		result["path"] = resolved
		result["workflow_relative_path"] = rawPath
		result["workflow_source_path"] = sourcePath
	}
	return result
}

// workflowRootDir returns the repo-root-relative directory that holds the
// workflow.json and its roles/, prompts/, and context files, derived from the
// snapshot's source_path. Empty when the workflow has no recorded source path or
// lives at the repo root. Self-driving lanes run from the repo root, so the
// packet surfaces this as the base for any workflow-declared relative path (#105).
func workflowRootDir(snapshot map[string]any) string {
	sourcePath, _ := snapshot["source_path"].(string)
	if sourcePath == "" || strings.HasPrefix(sourcePath, "/") {
		return ""
	}
	dir := path.Clean(path.Dir(sourcePath))
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}

// resolveWorkflowRelativePath maps a path declared relative to the workflow
// directory (e.g. roles/final_reviewer.md) to its repo-root-relative form
// (striatum/<wf>/roles/final_reviewer.md) so a lane running from the repo root
// can open it on the first try (#105). Returns the resolved repo-root-relative
// path and the original workflow-relative form. Absolute paths, an empty
// workflowRoot, and paths already prefixed with the workflow dir pass through.
func resolveWorkflowRelativePath(rawPath, workflowRoot string) (resolved, workflowRelative string) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" || strings.HasPrefix(rawPath, "/") || workflowRoot == "" {
		return rawPath, ""
	}
	cleaned := path.Clean(rawPath)
	if cleaned == workflowRoot || strings.HasPrefix(cleaned, workflowRoot+"/") {
		return cleaned, strings.TrimPrefix(cleaned, workflowRoot+"/")
	}
	joined := path.Clean(path.Join(workflowRoot, cleaned))
	if joined == "." || joined == ".." || strings.HasPrefix(joined, "../") {
		return rawPath, ""
	}
	return joined, cleaned
}

func workflowJobInterrogable(workflow map[string]any, workflowJobID string) bool {
	for _, item := range asList(workflow["jobs"]) {
		def := asMap(item)
		if fmt.Sprint(def["id"]) == workflowJobID {
			return def["interrogable"] == true
		}
	}
	return false
}

// revisionContextForPacket builds the RFC 0095 Goal 8 revision context for a
// re-opened verdict-capable (review) job: the prior attempt's finding artifact
// id and the prior verdict the reviewer recorded before the re-open. A fresh
// attempt (attempt 1) or a non-review job has no prior round and returns nil.
//
// Data availability (#206): the prior FINDING artifact persists (artifacts are
// attempt-scoped and append-only), so prior_finding_artifact_id is exact. The
// prior verdict is recovered from the durable `verdict.recorded` event — and
// since the current attempt has not yet recorded a verdict, the latest such
// event for the job is necessarily the prior round's. (RFC 0126 P0 / D194: the
// prior verdict ROW now also survives the re-open — verdict history is
// append-only, stamped per build generation — so the event read is no longer
// the only durable source, though it remains the source used here.) The
// free-text revision REASON is not separately recorded anywhere durable (it
// lived only on the verdict row's rationale), so it is omitted rather than
// fabricated.
func revisionContextForPacket(ctx context.Context, runner any, repositoryID string, job map[string]any) (map[string]any, error) {
	if !isVerdictCapableJobType(fmt.Sprint(job["job_type"])) {
		return nil, nil
	}
	currentAttempt := intValue(job["attempt"])
	if currentAttempt <= 1 {
		return nil, nil
	}
	jobID := fmt.Sprint(job["job_id"])
	runID := fmt.Sprint(job["run_id"])
	revision := map[string]any{
		"reopened_attempt": currentAttempt,
	}
	// Prior attempt's finding artifact (highest attempt below the current one).
	priorFinding, err := oneRow(ctx, runner, `
		SELECT artifact_id, logical_name, repo_path, content_sha256, attempt
		  FROM striatumd.artifacts
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND attempt < $4
		 ORDER BY attempt DESC, created_at DESC
		 LIMIT 1`, repositoryID, runID, jobID, currentAttempt)
	if err != nil && !errorsIsNoRows(err) {
		return nil, err
	}
	if err == nil {
		revision["prior_finding_artifact_id"] = priorFinding["artifact_id"]
		revision["prior_finding_logical_name"] = priorFinding["logical_name"]
		revision["prior_finding_path"] = priorFinding["repo_path"]
		revision["prior_finding_content_sha256"] = priorFinding["content_sha256"]
		revision["prior_finding_attempt"] = intValue(priorFinding["attempt"])
	}
	// Prior verdict from the durable verdict.recorded event (the verdict row was
	// deleted on re-open; the event is append-only).
	priorVerdictEvent, err := oneRow(ctx, runner, `
		SELECT payload_json
		  FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND event_type = 'verdict.recorded'
		 ORDER BY event_id DESC
		 LIMIT 1`, repositoryID, runID, jobID)
	if err != nil && !errorsIsNoRows(err) {
		return nil, err
	}
	if err == nil {
		if v := fmt.Sprint(asMap(priorVerdictEvent["payload_json"])["verdict"]); v != "" && v != "<nil>" {
			revision["prior_verdict"] = v
		}
	}
	// Carry the guidance the #206 daemon guard enforces so the lane self-corrects
	// before it trips the byte-identical refusal.
	revision["guidance"] = "This is a re-opened review round. Review the CURRENT revision of the target; do NOT republish the prior round's finding verbatim. A finding byte-identical to the prior attempt's is refused (fresh_review_byte_identical) — delete any stale finding file at prior_finding_path and write your own."
	return revision, nil
}

func interrogationTargetsForPacket(ctx context.Context, runner any, repositoryID, runID string, workflow map[string]any, consumerWorkflowJobID string) ([]map[string]any, error) {
	consumer := workflowJobDefinition(workflow, consumerWorkflowJobID)
	if consumer == nil {
		return nil, nil
	}
	targets := explicitInterrogationTargetsFromJob(consumer)
	if len(targets) == 0 {
		return nil, nil
	}
	entries := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		entry, err := interrogationTargetPacketEntry(ctx, runner, repositoryID, runID, target)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func workflowJobDefinition(workflow map[string]any, workflowJobID string) map[string]any {
	for _, item := range asList(workflow["jobs"]) {
		def := asMap(item)
		if fmt.Sprint(def["id"]) == workflowJobID {
			return def
		}
	}
	return nil
}

func interrogationTargetPacketEntry(ctx context.Context, runner any, repositoryID, runID string, target explicitInterrogationTarget) (map[string]any, error) {
	entry := map[string]any{
		"workflow_job_id": target.WorkflowJobID,
		"required":        target.Required,
	}
	targetJob, err := currentWorkflowJobRow(ctx, runner, repositoryID, runID, target.WorkflowJobID)
	if err != nil {
		return nil, err
	}
	if targetJob == nil {
		entry["state"] = "not_ready"
		entry["reason"] = nil
		entry["instruction"] = interrogationTargetInstruction("not_ready")
		return entry, nil
	}
	attempt := intValue(targetJob["attempt"])
	if attempt <= 0 {
		attempt = 1
	}
	entry["target_attempt"] = attempt
	if fmt.Sprint(targetJob["state"]) == "skipped" {
		entry["state"] = "unavailable"
		entry["reason"] = "target_skipped"
		entry["instruction"] = interrogationTargetInstruction("unavailable")
		return entry, nil
	}
	targetSessionID, err := interrogableTargetSessionForJob(ctx, runner, repositoryID, fmt.Sprint(targetJob["job_id"]))
	if err != nil {
		return nil, err
	}
	if targetSessionID == "" {
		entry["state"] = "not_ready"
		entry["reason"] = nil
		if reason, err := latestRetiredInterrogationTargetReason(ctx, runner, repositoryID, runID, target.WorkflowJobID); err != nil {
			return nil, err
		} else if reason == "revision_reopened" {
			entry["reason"] = reason
		}
		entry["instruction"] = interrogationTargetInstruction("not_ready")
		return entry, nil
	}
	entry["target_session_id"] = targetSessionID
	sessionRows, err := queryRows(ctx, runner, `
		SELECT state, close_reason
		  FROM striatumd.sessions
		 WHERE repository_id = $1 AND session_id = $2
		 LIMIT 1`, repositoryID, targetSessionID)
	if err != nil {
		return nil, err
	}
	if len(sessionRows) == 0 || fmt.Sprint(sessionRows[0]["state"]) != "active" {
		entry["state"] = "unavailable"
		reason := ""
		if len(sessionRows) > 0 {
			reason = fmt.Sprint(sessionRows[0]["close_reason"])
		}
		if reason == "" || reason == "<nil>" {
			reason = "target_retired"
		}
		entry["reason"] = reason
		entry["instruction"] = interrogationTargetInstruction("unavailable")
		return entry, nil
	}
	entry["state"] = "available"
	entry["reason"] = nil
	entry["instruction"] = interrogationTargetInstruction("available")
	return entry, nil
}

func currentWorkflowJobRow(ctx context.Context, runner any, repositoryID, runID, workflowJobID string) (map[string]any, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT job_id, workflow_job_id, attempt, state
		  FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2 AND workflow_job_id = $3
		 ORDER BY attempt DESC, created_at DESC
		 LIMIT 1`, repositoryID, runID, workflowJobID)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func latestRetiredInterrogationTargetReason(ctx context.Context, runner any, repositoryID, runID, workflowJobID string) (string, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT s.close_reason
		  FROM striatumd.events e
		  JOIN striatumd.jobs j
		    ON j.repository_id = e.repository_id AND j.job_id = e.job_id
		  JOIN striatumd.sessions s
		    ON s.repository_id = e.repository_id AND s.session_id = e.actor_session_id
		 WHERE e.repository_id = $1 AND e.run_id = $2
		   AND e.event_type = 'session.awaiting_interrogation'
		   AND j.workflow_job_id = $3
		 ORDER BY j.attempt DESC, e.event_id DESC
		 LIMIT 1`, repositoryID, runID, workflowJobID)
	if err != nil || len(rows) == 0 {
		return "", err
	}
	return fmt.Sprint(rows[0]["close_reason"]), nil
}

func interrogationTargetInstruction(state string) string {
	switch state {
	case "available":
		return "Open interrogation against target_session_id before recording findings."
	case "unavailable":
		return "The interrogation target is unavailable; proceed on the published artifact and record that interrogation was unavailable."
	default:
		return "The interrogation target is not ready; do not use a stale prior-attempt target_session_id."
	}
}

func laneWorktreeIsolation(workflow map[string]any, laneID string) string {
	if laneID == "" {
		return "off"
	}
	lane := asMap(asMap(workflow["lanes"])[laneID])
	mode, _ := lane["worktree_isolation"].(string)
	if mode == "per_job" {
		return "per_job"
	}
	return "off"
}

func downstreamImplementationEnvelopeForPacket(workflow map[string]any, workflowJobID string) map[string]any {
	workflowJobID = strings.TrimSpace(workflowJobID)
	if workflowJobID == "" {
		return nil
	}
	jobsByID := map[string]map[string]any{}
	for _, item := range asList(workflow["jobs"]) {
		job := asMap(item)
		id := strings.TrimSpace(fmt.Sprint(job["id"]))
		if id != "" {
			jobsByID[id] = job
		}
	}
	edgesByFrom := map[string][]string{}
	for _, item := range asList(workflow["edges"]) {
		edge := asMap(item)
		from := strings.TrimSpace(fmt.Sprint(edge["from"]))
		to := strings.TrimSpace(fmt.Sprint(edge["to"]))
		if from != "" && to != "" {
			edgesByFrom[from] = append(edgesByFrom[from], to)
		}
	}
	if len(edgesByFrom[workflowJobID]) == 0 {
		return nil
	}
	queue := append([]string(nil), edgesByFrom[workflowJobID]...)
	seen := map[string]bool{workflowJobID: true}
	entries := []any{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		if job := jobsByID[id]; len(job) > 0 {
			if entry := downstreamWorkflowJobEnvelope(job); entry != nil {
				entries = append(entries, entry)
			}
		}
		queue = append(queue, edgesByFrom[id]...)
	}
	if len(entries) == 0 {
		return nil
	}
	return map[string]any{
		"scope":       "reachable_downstream_jobs",
		"instruction": "When recommending implementation layout or follow-up work, stay within these downstream write_scope envelopes. If the correct implementation needs paths outside them, call that out explicitly instead of assuming the frozen scope can widen.",
		"jobs":        entries,
	}
}

func downstreamWorkflowJobEnvelope(job map[string]any) map[string]any {
	writeScope := asMap(job["write_scope"])
	expectedArtifacts := asList(job["expected_artifacts"])
	if len(writeScope) == 0 && len(expectedArtifacts) == 0 {
		return nil
	}
	entry := map[string]any{
		"workflow_job_id": job["id"],
		"type":            nullable(job["type"]),
		"title":           nullable(job["title"]),
		"role_id":         nullable(job["role_id"]),
		"lane_id":         nullable(downstreamWorkflowJobLaneID(job)),
		"write_scope":     writeScope,
	}
	if len(expectedArtifacts) > 0 {
		entry["expected_artifacts"] = expectedArtifacts
	}
	return entry
}

func downstreamWorkflowJobLaneID(job map[string]any) string {
	for _, key := range []string{"lane_id", "lane"} {
		if laneID, _ := job[key].(string); strings.TrimSpace(laneID) != "" {
			return laneID
		}
	}
	laneSelector := asMap(job["lane_selector"])
	laneID, _ := laneSelector["lane_id"].(string)
	return strings.TrimSpace(laneID)
}

func sharedResourcesForPacket(workflow map[string]any, workflowJobID string) map[string]any {
	workflowJobID = strings.TrimSpace(workflowJobID)
	if workflowJobID == "" {
		return nil
	}
	var currentJob map[string]any
	jobs := []map[string]any{}
	for _, item := range asList(workflow["jobs"]) {
		job := asMap(item)
		id := strings.TrimSpace(fmt.Sprint(job["id"]))
		if id == "" {
			continue
		}
		jobs = append(jobs, job)
		if id == workflowJobID {
			currentJob = job
		}
	}
	if len(currentJob) == 0 {
		return nil
	}
	resources := packetSharedResources(currentJob)
	if len(resources) == 0 {
		return nil
	}
	result := map[string]any{
		"scope":       "current_workflow_job",
		"instruction": "This job declares mutable shared resources. Before running validation or commands that mutate them, coordinate serialization for exclusive resources or use the declared per-lane namespace.",
		"resources":   resources,
	}
	parallelGroup := strings.TrimSpace(fmt.Sprint(currentJob["parallel_group"]))
	if parallelGroup == "" {
		return result
	}
	result["parallel_group"] = parallelGroup
	related := []any{}
	for _, job := range jobs {
		if strings.TrimSpace(fmt.Sprint(job["id"])) == workflowJobID || strings.TrimSpace(fmt.Sprint(job["parallel_group"])) != parallelGroup {
			continue
		}
		for _, overlap := range overlappingSharedResources(resources, packetSharedResources(job)) {
			related = append(related, map[string]any{
				"workflow_job_id": job["id"],
				"type":            nullable(job["type"]),
				"title":           nullable(job["title"]),
				"role_id":         nullable(job["role_id"]),
				"lane_id":         nullable(downstreamWorkflowJobLaneID(job)),
				"resource_id":     overlap["id"],
				"mode":            overlap["mode"],
				"namespace":       nullable(overlap["namespace"]),
			})
		}
	}
	if len(related) > 0 {
		result["related_parallel_jobs"] = related
	}
	return result
}

func packetSharedResources(job map[string]any) []map[string]any {
	resources := []map[string]any{}
	for _, item := range sharedResourceItems(job["shared_resources"]) {
		switch resource := item.(type) {
		case string:
			id := strings.TrimSpace(resource)
			if id == "" {
				continue
			}
			resources = append(resources, map[string]any{"id": id, "mode": "exclusive"})
		case map[string]any:
			id := strings.TrimSpace(fmt.Sprint(resource["id"]))
			if id == "" || id == "<nil>" {
				continue
			}
			mode, _ := resource["mode"].(string)
			if strings.TrimSpace(mode) == "" {
				mode = "exclusive"
			}
			entry := map[string]any{"id": id, "mode": mode}
			if description, _ := resource["description"].(string); strings.TrimSpace(description) != "" {
				entry["description"] = strings.TrimSpace(description)
			}
			if namespace, _ := resource["namespace"].(string); strings.TrimSpace(namespace) != "" {
				entry["namespace"] = strings.TrimSpace(namespace)
			}
			resources = append(resources, entry)
		}
	}
	return resources
}

func sharedResourceItems(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	}
	return nil
}

func overlappingSharedResources(left []map[string]any, right []map[string]any) []map[string]any {
	overlaps := []map[string]any{}
	for _, a := range left {
		for _, b := range right {
			if a["id"] != b["id"] {
				continue
			}
			aMode := resourceMode(a)
			bMode := resourceMode(b)
			aNamespace, _ := a["namespace"].(string)
			bNamespace, _ := b["namespace"].(string)
			if aMode == "exclusive" || bMode == "exclusive" || (aNamespace != "" && aNamespace == bNamespace) {
				overlaps = append(overlaps, map[string]any{
					"id":        a["id"],
					"mode":      aMode,
					"namespace": a["namespace"],
				})
			}
		}
	}
	return overlaps
}

func resourceMode(resource map[string]any) string {
	mode, _ := resource["mode"].(string)
	if strings.TrimSpace(mode) == "" {
		return "exclusive"
	}
	return mode
}

func expectedArtifactsWithAuthor(expected []any, authorLine string) []any {
	result := []any{}
	for _, item := range expected {
		artifact := asMap(item)
		if len(artifact) == 0 {
			result = append(result, item)
			continue
		}
		copy := map[string]any{}
		for key, value := range artifact {
			copy[key] = value
		}
		copy["author_line"] = authorLine
		result = append(result, copy)
	}
	return result
}

func augmentationReferences(workflow map[string]any, workflowJobID, repoRoot string) map[string]any {
	policy := asMap(workflow["augmentation"])
	if len(policy) == 0 || !stringListContains(asList(policy["jobs"]), workflowJobID) {
		return nil
	}
	budget := intValue(policy["budget_per_packet_lines"])
	if budget <= 0 {
		budget = 100
	}
	sources := []any{}
	for _, item := range asList(policy["sources"]) {
		source := augmentationCorpusBundleReference(repoRoot, asMap(item))
		if source != nil {
			sources = append(sources, source)
		}
	}
	return map[string]any{
		"mode":                    "reference_only",
		"required":                false,
		"budget_per_packet_lines": budget,
		"content_mode":            "references",
		"sources":                 sources,
	}
}

func stringListContains(items []any, needle string) bool {
	for _, item := range items {
		value, ok := item.(string)
		if ok && value == needle {
			return true
		}
	}
	return false
}

var augmentationSourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func augmentationCorpusBundleReference(repoRoot string, source map[string]any) map[string]any {
	if source["kind"] != "corpus_bundle" {
		return nil
	}
	sourceID, _ := source["id"].(string)
	relPath := safeAugmentationPath(fmt.Sprint(source["path"]))
	if sourceID == "" || relPath == "" {
		return nil
	}
	manifestRel := path.Join(relPath, "manifest.json")
	view := map[string]any{
		"source_id":     sourceID,
		"kind":          "corpus_bundle",
		"path":          relPath,
		"manifest_path": manifestRel,
		"fetch_mode":    "agent_side_local_bundle",
		"required":      false,
	}
	if description, _ := source["description"].(string); description != "" {
		view["description"] = description
	}
	bundlePath := filepath.Join(repoRoot, filepath.FromSlash(relPath))
	manifestPath := filepath.Join(bundlePath, "manifest.json")
	info, err := os.Stat(bundlePath)
	if err != nil {
		reason := "bundle_unavailable"
		status := "unavailable"
		if os.IsNotExist(err) {
			reason = "bundle_not_found"
			status = "missing"
		}
		view["status"] = status
		view["available"] = false
		view["reason"] = reason
		return view
	}
	if !info.IsDir() {
		view["status"] = "unavailable"
		view["available"] = false
		view["reason"] = "bundle_not_directory"
		return view
	}
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		reason := "manifest_unreadable"
		status := "unavailable"
		if os.IsNotExist(err) {
			reason = "manifest_not_found"
			status = "missing"
		}
		view["status"] = status
		view["available"] = false
		view["reason"] = reason
		return view
	}
	var manifest map[string]any
	if err := json.Unmarshal(body, &manifest); err != nil || manifest == nil {
		view["status"] = "unavailable"
		view["available"] = false
		view["reason"] = "manifest_unreadable"
		return view
	}
	view["status"] = "available"
	view["available"] = true
	view["manifest"] = augmentationManifestSummary(manifest)
	return view
}

func safeAugmentationPath(raw string) string {
	if raw == "" || strings.HasPrefix(raw, "/") || strings.Contains(raw, "://") {
		return ""
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	if cleaned == ".striatum" || strings.HasPrefix(cleaned, ".striatum/") {
		return ""
	}
	return cleaned
}

func augmentationManifestSummary(manifest map[string]any) map[string]any {
	summary := map[string]any{}
	for _, key := range []string{
		"corpus_contract_version",
		"corpus_id",
		"redaction_tier",
		"verification_depth",
		"bundle_sha256",
	} {
		switch value := manifest[key].(type) {
		case string:
			summary[key] = value
		case float64:
			summary[key] = value
		case int:
			summary[key] = value
		}
	}
	if rowCounts := asMap(manifest["row_counts"]); len(rowCounts) > 0 {
		summary["row_counts"] = rowCounts
	}
	return summary
}

func buildPacketCommands(sessionID, jobID, messageID, leaseID string, worktreeRequired bool) map[string]any {
	commands := map[string]any{
		"ack":              fmt.Sprintf("striatum ack --session-id %s --message-id %s --lease-id %s", sessionID, messageID, leaseID),
		"heartbeat":        fmt.Sprintf("striatum heartbeat --session-id %s --lease-id %s", sessionID, leaseID),
		"publish_artifact": fmt.Sprintf("striatum publish-artifact --session-id %s --job-id %s --lease-id %s", sessionID, jobID, leaseID),
		"block":            fmt.Sprintf("striatum block --session-id %s --job-id %s --lease-id %s", sessionID, jobID, leaseID),
		"verdict":          fmt.Sprintf("striatum verdict --session-id %s --job-id %s --lease-id %s", sessionID, jobID, leaseID),
		"complete":         fmt.Sprintf("striatum complete --session-id %s --job-id %s --lease-id %s", sessionID, jobID, leaseID),
	}
	if worktreeRequired {
		commands["worktree_create"] = fmt.Sprintf("striatum worktree create --session-id %s --job-id %s --lease-id %s", sessionID, jobID, leaseID)
	}
	return commands
}

func buildAdapterConstraints(laneConfig map[string]any) map[string]any {
	adapter := laneConfig["adapter"]
	constraints := asMap(laneConfig["constraints"])
	required := asMap(laneConfig["required_enforcement"])
	enforcement := []any{}
	for key, value := range constraints {
		requested, ok := value.(string)
		if !ok {
			continue
		}
		actual := adapterConstraintEnforcement(adapter, key, requested)
		requiredText, _ := required[key].(string)
		enforcement = append(enforcement, map[string]any{
			"constraint":           key,
			"requested":            requested,
			"required_enforcement": nullable(requiredText),
			"enforcement":          actual,
			"satisfied":            requiredText == "" || adapterEnforcementSatisfies(actual, requiredText),
		})
	}
	satisfied := true
	for _, item := range enforcement {
		if asMap(item)["satisfied"] != true {
			satisfied = false
			break
		}
	}
	return map[string]any{
		"requested":            constraints,
		"required_enforcement": required,
		"enforcement":          enforcement,
		"satisfied":            satisfied,
	}
}

func adapterConstraintEnforcement(adapter any, constraint, requested string) string {
	if adapter == "process" {
		if constraint == "transcripts" && requested == "off" {
			return "enforced"
		}
		if constraint == "network" && requested == "forbidden" {
			return "advisory_strict"
		}
		if constraint == "repo_scope" && requested == "local_only" {
			return "advisory_strict"
		}
		return "advisory"
	}
	return "unsupported"
}

func adapterEnforcementSatisfies(actual, required string) bool {
	rank := map[string]int{"unsupported": 0, "advisory": 1, "advisory_strict": 2, "enforced": 3}
	return rank[actual] >= rank[required]
}

func harnessProfileView(workflow map[string]any, laneID string) map[string]any {
	if laneID == "" {
		return nil
	}
	lane := asMap(asMap(workflow["lanes"])[laneID])
	profileID, _ := lane["harness_profile_id"].(string)
	if profileID == "" {
		return nil
	}
	body := asMap(asMap(workflow["harness_profiles"])[profileID])
	if len(body) == 0 {
		return nil
	}
	view := map[string]any{"profile_id": profileID}
	for key, value := range body {
		view[key] = value
	}
	return view
}

var reviewAccessInstructions = map[string]string{
	"document_only":      "Read only the target documents listed in inputs. Do not consult other artifacts, ledgers, reports, or repository contents beyond inputs.",
	"artifact_augmented": "You may read the target documents AND the supporting artifacts/reports/ledgers listed in inputs. Do not inspect other repository contents.",
	"repo_level":         "You may inspect the repository within the job's declared write_scope.allowed_paths/forbidden_paths. Stay within that scope.",
}

var reviewContextInstructions = map[string]string{
	"fresh":       " This is a fresh-context review. Do not rely on prior thread state from earlier rounds.",
	"cross_round": " You may retain prior context to verify whether previously raised issues were resolved.",
}

var reviewPostureInstructions = map[string]string{
	"neutral":             "",
	"devils_advocate":     " This is a devil's-advocate review. Argue against the artifact's claims; verdict acceptance means the claims survived your strongest counterarguments.",
	"security":            " This is a security-focused review. Read the artifact looking for security weaknesses; verdict acceptance means you actively looked and found nothing actionable.",
	"threat_model":        " This is a threat-modeling review. Enumerate the trust boundaries and attack surfaces the artifact introduces; verdict acceptance means each is acknowledged or mitigated.",
	"latency_performance": " This is a latency / performance review. Evaluate the artifact's runtime and resource cost; verdict acceptance means no acceptance-blocking regression was found.",
	"ergonomics_dx":       " This is a developer-ergonomics review. Evaluate the artifact's surface from a first-time-user perspective; verdict acceptance means the affordances are discoverable and consistent.",
	"accessibility":       " This is an accessibility review. Evaluate the artifact against accessibility expectations; verdict acceptance means the affordances meet the declared accessibility bar.",
	"compliance_license":  " This is a compliance / license review. Evaluate the artifact for license, attribution, or compliance issues; verdict acceptance means none are unresolved.",
	"supply_chain":        " This is a supply-chain review. Evaluate the artifact's external dependencies and their provenance; verdict acceptance means each is justified and pinned.",
}

func buildReviewPolicy(workflow map[string]any, workflowJobID string) map[string]any {
	for _, item := range asList(workflow["jobs"]) {
		job := asMap(item)
		if job["id"] != workflowJobID || job["type"] != "review" {
			continue
		}
		_, hasAccess := job["reviewer_access_scope"]
		_, hasContext := job["reviewer_context_policy"]
		_, hasPosture := job["review_posture"]
		if !hasAccess && !hasContext && !hasPosture {
			return nil
		}
		access, _ := job["reviewer_access_scope"].(string)
		if access == "" {
			access = "document_only"
		}
		contextPolicy, _ := job["reviewer_context_policy"].(string)
		if contextPolicy == "" {
			contextPolicy = "cross_round"
		}
		posture, _ := job["review_posture"].(string)
		if posture == "" {
			posture = "neutral"
		}
		accessText, ok := reviewAccessInstructions[access]
		if !ok {
			return nil
		}
		contextText, ok := reviewContextInstructions[contextPolicy]
		if !ok {
			return nil
		}
		block := map[string]any{
			"access_scope":   access,
			"context_policy": contextPolicy,
			"instruction":    accessText + contextText + reviewPostureInstructions[posture],
		}
		if hasPosture {
			block["posture"] = posture
		}
		return block
	}
	return nil
}

func artifactAuthorIdentity(workflow map[string]any, roleID, laneID, workflowJobID string, ordinal int, attested bool, operatorLabel string) map[string]any {
	displayModel := laneDisplayModel(workflow, laneID)
	var line any
	if ordinal > 0 {
		if attested {
			model := displayModel
			if model == "" {
				model = "unknown-model"
			}
			line = fmt.Sprintf("author: %s-%s-%03d", authorPart(roleID), authorPart(model), ordinal)
		} else {
			line = operatorAuthorLine(operatorLabel)
		}
	}
	return map[string]any{
		"role_id":         roleID,
		"lane_id":         nullable(laneID),
		"display_model":   nullable(displayModel),
		"workflow_job_id": workflowJobID,
		"ordinal":         ordinal,
		"line":            line,
	}
}

func laneDisplayModel(workflow map[string]any, laneID string) string {
	if laneID == "" {
		return ""
	}
	model, _ := asMap(asMap(workflow["lanes"])[laneID])["display_model"].(string)
	return model
}

var authorPartPattern = regexp.MustCompile(`[^a-z0-9.]+`)

func authorPart(value string) string {
	normalized := strings.Trim(authorPartPattern.ReplaceAllString(strings.ToLower(value), "-"), "-")
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

func operatorAuthorLine(operatorLabel string) string {
	if operatorLabel == "" || operatorLabel == "<nil>" {
		return "author: operator"
	}
	return fmt.Sprintf("author: operator-self-declared-%s", operatorLabel)
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	}
	return 0
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int32:
		return typed != 0
	case int64:
		return typed != 0
	case string:
		return typed == "true" || typed == "1"
	}
	return false
}

// awaitClaimNext is the await loop's claim step, indirected through a package
// var so a test can inject a transient daemon-load error (SQLSTATE 57014) without
// standing up real Postgres statement-timeout contention (#197). It defaults to
// the production HandleClaimNext.
var awaitClaimNext = HandleClaimNext

// awaitPacketTimeout and awaitPacketPollInterval bound the long-poll loop. They
// are package vars so a test can shrink the loop to exercise the deadline path
// (e.g. the #197 transient-load classification) without a 30s wall-clock wait.
var (
	awaitPacketTimeout      = 30 * time.Second
	awaitPacketPollInterval = 500 * time.Millisecond
)

func HandleAwaitPacket(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	if sessionID == "" {
		return nil, rpc.NewError("schema_invalid", "work.await_packet requires session_id", nil)
	}
	// RFC 0096 V2 / #135: a bound token may only await its own session's work.
	if _, err := enforceSessionBinding(ctx, sessionID); err != nil {
		return nil, err
	}
	// RFC 0095 §4 (F-I/#81): refuse the await loop for a closed/expired/lost
	// session before it can be delivered work, an interrogation question, or a
	// conversation turn. Every non-active state — stopped (the #245 first-await
	// race: the supervised process already exited) and closed/expired/lost
	// alike — returns an in-band terminal envelope rather than an RPC error,
	// because the agent-loop receiver retries any await error after a backoff
	// and would otherwise poll a terminal session forever (RFC 0120 Phase 1).
	// The gate runs before the liveness write so a terminal session probing
	// await does not keep refreshing last_await_packet_at.
	if state, err := sessionState(ctx, runner, repositoryID, sessionID); err != nil {
		return nil, err
	} else if state != "active" {
		return awaitTerminalSessionEnvelope(state), nil
	}
	if err := sessionliveness.Record(ctx, runner, repositoryID, sessionID, sessionliveness.LastAwaitPacketAt); err != nil {
		return nil, err
	}

	timeout := awaitPacketTimeout
	pollInterval := awaitPacketPollInterval
	deadline := time.Now().Add(timeout)
	sawTransientLoad := false

	for {
		backendReady, backendReason, err := awaitSessionBackendReady(ctx, runner, repositoryID, sessionID)
		if err != nil {
			return nil, err
		}
		if !backendReady {
			if time.Now().After(deadline) {
				env := awaitNoneEnvelope()
				env["reason"] = "session_backend_not_ready"
				env["backend_reason"] = backendReason
				env["hint"] = "session backend is not attached yet; keep queued work unclaimed and let supervise.start finish or run drive register/start a replacement session"
				return env, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(pollInterval):
			}
			continue
		}

		// RFC 0082: a worker's single subscribe loop receives either work or a
		// pending interrogation question addressed to its session. Delivery
		// prefers a pending interrogation question over new work so
		// interrogations are answered promptly.
		question, err := deliverPendingInterrogationQuestion(ctx, runner, repositoryID, sessionID)
		if err != nil {
			return nil, err
		}
		if question != nil {
			return question, nil
		}

		// RFC 0086: a participant's await loop also receives its round-robin
		// conversation turn. Preference: a direct peer question (interrogation)
		// over a group turn (conversation) over new work.
		convTurn, err := deliverPendingConversationTurn(ctx, runner, repositoryID, sessionID)
		if err != nil {
			return nil, err
		}
		if convTurn != nil {
			return convTurn, nil
		}

		res, err := awaitClaimNext(ctx, runner, envelope)
		if err != nil {
			// #197: a claim query canceled for exceeding statement_timeout
			// (SQLSTATE 57014) under concurrent multi-run load — or a transient
			// connection teardown (class 57) — is "the daemon is busy, poll again",
			// not a real failure. Propagating it raw surfaced
			// `ERROR: canceling statement due to statement timeout (SQLSTATE 57014)`
			// to the lane, which models read as broken and stopped retrying,
			// orphaning the job. The await loop has its own deadline, so swallow the
			// transient error and poll again; if every attempt to the deadline was
			// transient we return a legible no_work envelope explaining why instead
			// of an SQL error.
			if isTransientDaemonLoadError(err) {
				sawTransientLoad = true
				if time.Now().After(deadline) {
					env := awaitNoneEnvelope()
					env["reason"] = "transient_daemon_load"
					return env, nil
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(pollInterval):
				}
				continue
			}
			return nil, err
		}

		status := fmt.Sprint(res["status"])
		if status == "claimed" {
			return awaitWorkEnvelope(res), nil
		}

		// #107: if the only remaining work for this role is structurally
		// ineligible for this session (e.g. a fresh_session_required job a spent
		// session can never claim), surface the reason and stop polling — the
		// session will not become eligible, so the coordinator should register a
		// fresh session instead of the lane polling no_work until the deadline.
		if reason, _ := res["ineligible_reason"].(string); reason != "" {
			env := awaitNoneEnvelope()
			env["ineligible_reason"] = reason
			if v, ok := res["workflow_job_id"]; ok {
				env["workflow_job_id"] = v
			}
			if v, ok := res["blocker_id"]; ok {
				env["blocker_id"] = v
			}
			if v, ok := res["hint"]; ok {
				env["hint"] = v
			}
			// The session can never claim the remaining work; the lane exits. Stamp
			// the durable clean-exit signal so the sweep can reclaim it later (#302).
			return idleExitEnvelope(ctx, runner, repositoryID, sessionID, env), nil
		}

		isRunning, err := isRunRunning(ctx, runner, repositoryID, sessionID)
		if err != nil {
			return nil, err
		}
		if !isRunning {
			// The run is finished and this lane is exiting (the #302 post-publish
			// shape): record the durable terminal pointer state so a falsely-active
			// session is still reclaimable after the pane/helper tears down.
			return idleExitEnvelope(ctx, runner, repositoryID, sessionID, awaitNoneEnvelope()), nil
		}

		if time.Now().After(deadline) {
			env := awaitNoneEnvelope()
			if sawTransientLoad {
				env["reason"] = "transient_daemon_load"
			}
			// No work is available for this session and the await deadline elapsed:
			// the lane exits per the idle-exit contract. A transient-load deadline is
			// NOT a clean exit, so only the genuine no-work idle records the signal.
			if !sawTransientLoad {
				return idleExitEnvelope(ctx, runner, repositoryID, sessionID, env), nil
			}
			return env, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// awaitWorkEnvelope wraps a claim_next result in the RFC 0082 typed envelope
// while preserving the legacy fields (status, packet_id, packet, next_steps)
// so existing callers that read those keys keep working.
func awaitWorkEnvelope(res map[string]any) map[string]any {
	out := map[string]any{"type": "work_packet"}
	for key, value := range res {
		out[key] = value
	}
	return out
}

func awaitSessionBackendReady(ctx context.Context, runner any, repositoryID, sessionID string) (bool, string, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT ps.state AS state
		  FROM striatumd.process_supervisors ps
		 WHERE ps.repository_id = $1
		   AND ps.session_id = $2
		   AND ps.state IN ('starting','attached','detached')
		 ORDER BY ps.started_at DESC, ps.supervisor_id DESC
		 LIMIT 1`, repositoryID, sessionID)
	if err != nil {
		return false, "", err
	}
	if len(rows) == 0 {
		return false, "supervisor_not_started", nil
	}
	state := fmt.Sprint(rows[0]["state"])
	if state != "attached" {
		return false, "supervisor_" + state, nil
	}
	return true, "", nil
}

// freshSessionBlockedWorkflowJob returns the workflow_job_id of a pending job
// for this (role, lane) that is fresh_session_required and that the given
// session cannot claim because it has already worked another job in this run
// (the inverse of the claim eligibility filter). Empty when no such job exists.
// Used to explain a no_work response instead of leaving the lane to poll
// forever (#107).
func freshSessionBlockedWorkflowJob(ctx context.Context, runner any, repositoryID, roleID, laneID, sessionID, runID string) string {
	row, err := oneRow(ctx, runner, `
		SELECT j.workflow_job_id AS workflow_job_id
		  FROM striatumd.queue_messages qm
		  JOIN striatumd.jobs j
		    ON j.repository_id = qm.repository_id AND j.job_id = qm.job_id
		 WHERE qm.repository_id = $1
		   AND qm.kind = 'work'
		   AND qm.state = 'pending'
		   AND qm.target_role_id = $2
		   AND (qm.target_lane_id IS NULL OR qm.target_lane_id = $3)
		   AND qm.run_id = $5
		   AND j.fresh_session_required = true
		   AND EXISTS (
		     SELECT 1 FROM striatumd.work_packets wp
		      WHERE wp.repository_id = qm.repository_id
		        AND wp.run_id = qm.run_id
		        AND wp.session_id = $4
		        AND wp.job_id != qm.job_id
		   )
		 ORDER BY qm.priority DESC, qm.created_at ASC
		 LIMIT 1`,
		repositoryID, roleID, laneID, sessionID, runID)
	if err != nil {
		return ""
	}
	return fmt.Sprint(row["workflow_job_id"])
}

// openEscalationBlockerForRun returns the id of an open escalation-inbox row for
// the run (the thing an operator resolves to clear needs_operator), best-effort:
// empty string on any error or when none exists. Used to enrich the #207 part A
// run_needs_operator claim response so the operator knows exactly what to resolve.
func openEscalationBlockerForRun(ctx context.Context, runner any, repositoryID, runID string) string {
	row, err := oneRow(ctx, runner, `
		SELECT escalation_id
		  FROM striatumd.escalation_inbox
		 WHERE repository_id = $1 AND run_id = $2 AND state <> 'resolved'
		 ORDER BY created_at DESC
		 LIMIT 1`, repositoryID, runID)
	if err != nil {
		return ""
	}
	return fmt.Sprint(row["escalation_id"])
}

func awaitNoneEnvelope() map[string]any {
	return map[string]any{
		"type":          "none",
		"status":        "no_work",
		"idle_behavior": "exit_session",
		"hint":          "no work is available for this session; stop this lane and let run drive or a future wake mechanism start a fresh session when work is queued",
	}
}

// awaitTerminalSessionEnvelope is the in-band terminal result work.await_packet
// returns for any non-active session state (stopped, closed, expired, lost).
// The reason keeps the established "session_stopped" spelling for stopped
// sessions and extends the same "session_<state>" shape to the rest, so the
// agent-loop receiver always sees a no_work/exit_session envelope instead of a
// retryable RPC error (RFC 0120 Phase 1).
func awaitTerminalSessionEnvelope(state string) map[string]any {
	return map[string]any{
		"type":          "session_terminal",
		"status":        "no_work",
		"reason":        "session_" + state,
		"session_state": state,
		"idle_behavior": "exit_session",
		"next_action":   "register_fresh_session",
		"hint":          fmt.Sprintf("this session is %s; stop this lane and let run drive register and start a fresh session for remaining work", state),
	}
}

// idleExitEnvelope records the durable clean-lane-exit signal for sessionID and
// returns env unchanged. It is the chokepoint for every work.await_packet result
// that carries the idle_behavior=exit_session contract: by returning that
// envelope the daemon has TOLD this lane to stop, so the lane is contractually
// about to exit (the agent-loop receiver SIGTERMs the agent and returns; codex's
// own loop stops). recordCleanLaneExitSignal stamps the lane's supervisor pointer
// terminal BEFORE the pane/helper tears down, so the recovery sweep can still
// reclaim a falsely-active session even after a live tmux/PID probe is no longer
// possible (#302 residual). A best-effort recording failure must never turn a
// no_work answer into an RPC error (that would leave the receiver error-looping
// against a finished session), so the error is swallowed; an unrecorded signal
// only loses the durability optimization, it never wedges the lane more than the
// pre-existing behavior did.
func idleExitEnvelope(ctx context.Context, runner db.Runner, repositoryID, sessionID string, env map[string]any) map[string]any {
	_ = recordCleanLaneExitSignal(ctx, runner, repositoryID, sessionID)
	return env
}

// recordCleanLaneExitSignal stamps a DURABLE terminal dead-signal for a lane that
// the daemon has just told to idle-exit (work.await_packet -> no_work /
// exit_session, RFC 0120 Phase 1). It flips the session's most-recent
// non-terminal supervisor pointer to state='stopped' so that, once the lane
// exits and the tmux pane dies / the helper is torn down, the recovery sweep's
// supervisedAgentConfirmedDead reads a conclusive 'stopped' pointer (an
// unforgeable recorded-exit fact) instead of an ambiguous live probe —
// reclaiming the session + requeuing any still-queued job (#302) even when no
// live probe is possible.
//
// This is recorded ONLY when the daemon itself issued the exit contract; it is
// NOT inferred from an ambiguous/unavailable probe, so the #145/#147
// false-requeue guard is untouched: a session with an unreadable probe and NO
// recorded terminal pointer is still treated as possibly-live and left alone.
//
// Safety properties:
//   - The SESSION row is left state='active'. Only the supervisor POINTER is
//     marked terminal. The sweep's #291 queued-scan acts solely on an active
//     bound session; marking the session itself stopped here would make the
//     sweep skip it and strand the queued job. The sweep is the component that
//     closes the session, exactly as the existing #302 dead-pane test asserts.
//   - The pointer update is guarded to flip only a CURRENTLY non-terminal
//     pointer in {'attached','detached'}. A 'starting' supervisor that has not
//     attached yet is left alone (its lane never ran), and an already
//     'stopped'/'lost' pointer is untouched, so the write is idempotent.
//   - The write runs in its own short transaction so it cannot poison the
//     long-poll await loop.
func recordCleanLaneExitSignal(ctx context.Context, runner db.Runner, repositoryID, sessionID string) error {
	if repositoryID == "" || sessionID == "" {
		return nil
	}
	_, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		now := nowString()
		// Resolve the lane's most-recent non-terminal supervisor pointer for an
		// event/audit trail; absent any such pointer there is nothing durable to
		// stamp (a never-supervised session leaves no recoverable lane signal).
		row, err := oneRow(ctx, tx, `
			SELECT supervisor_id, run_id
			  FROM striatumd.process_supervisor_pointers
			 WHERE repository_id = $1
			   AND session_id = $2
			   AND state IN ('attached','detached')
			 ORDER BY updated_at DESC, supervisor_id DESC
			 LIMIT 1`, repositoryID, sessionID)
		if err != nil {
			if isNoRows(err) {
				return map[string]any{}, nil
			}
			return nil, err
		}
		supervisorID := fmt.Sprint(row["supervisor_id"])
		if supervisorID == "" || supervisorID == "<nil>" {
			return map[string]any{}, nil
		}
		runID := nullable(row["run_id"])
		if err := tx.Exec(ctx, `
			UPDATE striatumd.process_supervisor_pointers
			   SET state = 'stopped',
			       updated_at = $1
			 WHERE repository_id = $2
			   AND supervisor_id = $3
			   AND state IN ('attached','detached')`,
			now, repositoryID, supervisorID); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "supervisor.lane_exited", sessionID, nil, nil, nil, nil, map[string]any{
			"session_id":    sessionID,
			"supervisor_id": supervisorID,
			"reason":        "idle_exit_no_work",
			"source":        "await_packet",
		}); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	})
	return err
}

// deliverPendingInterrogationQuestion returns the oldest pending interrogation
// question addressed to this session and marks it delivered (acked). It returns
// nil when none is pending. The question is delivered to the target session's
// receive loop and to no other session (its target_session_id is the filter).
func deliverPendingInterrogationQuestion(ctx context.Context, runner db.Runner, repositoryID, sessionID string) (map[string]any, error) {
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		rows, err := queryRows(ctx, tx, `
			SELECT message_id, payload_json
			  FROM striatumd.queue_messages
			 WHERE repository_id = $1
			   AND target_session_id = $2
			   AND kind = 'agent_message'
			   AND state = 'pending'
			   AND payload_json->>'turn' = 'question'
			   AND payload_json->>'interrogation_id' IS NOT NULL
			 ORDER BY created_at ASC, message_id ASC
			 LIMIT 1
			 FOR UPDATE SKIP LOCKED`,
			repositoryID, sessionID)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, nil
		}
		row := rows[0]
		messageID := fmt.Sprint(row["message_id"])
		payload := asMap(row["payload_json"])
		interrogationID := fmt.Sprint(payload["interrogation_id"])
		body := fmt.Sprint(payload["body"])
		now := nowString()
		if err := tx.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'acked', acked_at = $1, updated_at = $1
			 WHERE repository_id = $2 AND message_id = $3`,
			now, repositoryID, messageID); err != nil {
			return nil, err
		}
		return map[string]any{
			"type":             "interrogation_question",
			"status":           "interrogation_question",
			"interrogation_id": interrogationID,
			"message_id":       messageID,
			"body":             body,
		}, nil
	})
}

// sessionState returns the current sessions.state value for sessionID.
func sessionState(ctx context.Context, runner db.Runner, repositoryID, sessionID string) (string, error) {
	row, err := oneRow(ctx, runner, `
		SELECT state FROM striatumd.sessions
		 WHERE repository_id = $1 AND session_id = $2`, repositoryID, sessionID)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(row["state"]), nil
}

func isRunRunning(ctx context.Context, runner db.Runner, repositoryID, sessionID string) (bool, error) {
	var state string
	var paused bool
	_, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		session, err := rowByID(ctx, tx, repositoryID, "sessions", "session_id", sessionID, true)
		if err != nil {
			return nil, err
		}
		runID := fmt.Sprint(session["run_id"])
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true)
		if err != nil {
			return nil, err
		}
		state = fmt.Sprint(run["state"])
		paused = nullable(run["paused_at"]) != nil
		return nil, nil
	})
	if err != nil {
		return false, err
	}
	return state == "running" && !paused, nil
}
