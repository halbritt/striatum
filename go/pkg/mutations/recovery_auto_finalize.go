package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

const (
	defaultAutoFinalizeMtimeGraceSeconds = 30
	defaultAutoFinalizeBreakerMax        = 3
	defaultAutoFinalizeBreakerWindow     = 600
	autoFinalizeSummary                  = "auto-finalized from stable expected artifact files"
	noProcessAutoFinalizeRationale       = "auto-finalized from stable expected artifact front matter; no terminal output or transcript was used as evidence"

	autoFinalizeCauseFinalizePublishFailed  = "finalize_publish_failed"
	autoFinalizeCauseFinalizeCompleteFailed = "finalize_complete_failed"
	autoFinalizeCauseFinalizeVerdictFailed  = "finalize_verdict_failed"
	autoFinalizeCauseFinalizeUnexpected     = "finalize_unexpected_error"
	autoFinalizeCauseCircuitBreakerOpen     = "circuit_breaker_open"

	// #274 (RFC 0125 P1-3): skip-record reasons for jobs the live-lease candidate
	// join filters out (blocked / lease-released). These give the operator a
	// queryable cause and a concrete recovery pointer instead of a silent
	// eligible_count:0 with an empty skipped[].
	autoFinalizeReasonPublishedArtifactNotDurable = "published_artifact_not_durable"
	autoFinalizeReasonBlocked                     = "blocked"
)

func HandleRecoveryAutoFinalize(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "recovery.auto_finalize requires run_id", nil)
	}
	dryRun, err := autoFinalizeDryRun(envelope)
	if err != nil {
		return nil, err
	}
	mtimeGraceSeconds, err := autoFinalizeMtimeGraceSeconds(envelope)
	if err != nil {
		return nil, err
	}
	if boolParam(envelope, "reset_circuit_breaker") && dryRun {
		return nil, rpc.NewError("invalid_transition", "--reset-circuit-breaker requires --live", nil)
	}
	policy, err := readAutoFinalizePolicy(ctx, runner, repositoryID, runID, envelope, mtimeGraceSeconds)
	if err != nil {
		return nil, err
	}
	if !dryRun && policy["live_allowed"] != true {
		return nil, rpc.NewError("invalid_transition", "recovery.auto_finalize live mode is disabled by workflow recovery.auto_finalize.enabled=false; use --force for operator override", nil)
	}
	var resetResult map[string]any
	if boolParam(envelope, "reset_circuit_breaker") {
		resetResult, err = resetAutoFinalizeCircuitBreakers(ctx, runner, repositoryID, runID, stringParam(envelope, "job_id"))
		if err != nil {
			return nil, err
		}
		policy, err = readAutoFinalizePolicy(ctx, runner, repositoryID, runID, envelope, mtimeGraceSeconds)
		if err != nil {
			return nil, err
		}
	}

	if dryRun {
		return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
			run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, false)
			if err != nil {
				return nil, err
			}
			if fmt.Sprint(run["state"]) != "running" {
				return autoFinalizeEmptyResult(runID, true, fmt.Sprintf("run is not running: %s", run["state"]), policy), nil
			}
			rows, err := autoFinalizeCandidateRows(ctx, tx, repositoryID, runID, stringParam(envelope, "job_id"))
			if err != nil {
				return nil, err
			}
			result, err := evaluateAutoFinalizeRows(ctx, tx, repositoryID, fmt.Sprint(run["repo_root"]), runID, rows, mtimeGraceSeconds, boolParam(envelope, "allow_no_process_execution"), policy)
			if err != nil {
				return nil, err
			}
			// #274 (RFC 0125 P1-3): a blocked / lease-released job is filtered out
			// of the live-lease candidate join above, so without this the operator
			// sees eligible_count:0 with an empty skipped[] and no pointer to a
			// recovery path. Surface those jobs as EXPLICIT skip records explaining
			// WHY auto-finalize did not consider them (and pointing at recovery
			// reseal when the cause is an undurable published artifact).
			if err := appendAutoFinalizeBlockedJobSkips(ctx, tx, repositoryID, fmt.Sprint(run["repo_root"]), runID, stringParam(envelope, "job_id"), result); err != nil {
				return nil, err
			}
			if resetResult != nil {
				result["circuit_breaker_reset"] = resetResult
			}
			return result, nil
		})
	}

	rows, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, false)
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(run["state"]) != "running" {
			return autoFinalizeEmptyResult(runID, false, fmt.Sprintf("run is not running: %s", run["state"]), policy), nil
		}
		candidates, err := autoFinalizeCandidateRows(ctx, tx, repositoryID, runID, stringParam(envelope, "job_id"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"repo_root": run["repo_root"], "rows": candidates}, nil
	})
	if err != nil {
		return nil, err
	}
	if _, ok := rows["eligible_count"]; ok {
		return rows, nil
	}

	repoRoot := fmt.Sprint(rows["repo_root"])
	finalized := []map[string]any{}
	skipped := []map[string]any{}
	for _, item := range asList(rows["rows"]) {
		row := asMap(item)
		jobID := fmt.Sprint(row["job_id"])
		result, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
			// RFC 0104: per-run advisory lock first — auto-finalize completes a job
			// (maybeCompleteRun -> closeRemainingSessions), inverting against claim.
			if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
				return nil, err
			}
			openBreaker, err := openAutoFinalizeCircuitBreakerForJob(ctx, tx, repositoryID, runID, fmt.Sprint(row["workflow_job_id"]))
			if err != nil {
				return nil, err
			}
			if openBreaker != nil {
				return map[string]any{
					"eligible": false,
					"skip":     autoFinalizeCircuitBreakerSkip(row, openBreaker),
				}, nil
			}
			fresh, err := autoFinalizeCandidateRows(ctx, tx, repositoryID, runID, jobID)
			if err != nil {
				return nil, err
			}
			if len(fresh) == 0 {
				return map[string]any{
					"eligible": false,
					"skip": map[string]any{
						"workflow_job_id": row["workflow_job_id"],
						"job_id":          jobID,
						"reason":          "candidate is no longer auto-finalizable",
					},
				}, nil
			}
			evaluation := evaluateAutoFinalizeCandidate(ctx, tx, repositoryID, repoRoot, fresh[0], mtimeGraceSeconds, boolParam(envelope, "allow_no_process_execution"))
			if evaluation["eligible"] != true {
				return evaluation, nil
			}
			finalizedCandidate, err := finalizeAutoFinalizeCandidate(ctx, tx, repositoryID, evaluation["candidate"].(map[string]any), boolParam(envelope, "allow_no_process_execution"))
			if err != nil {
				return nil, err
			}
			if _, err := clearAutoFinalizeCircuitBreakersForJob(ctx, tx, repositoryID, runID, fmt.Sprint(row["workflow_job_id"])); err != nil {
				return nil, err
			}
			return map[string]any{"eligible": true, "finalized": finalizedCandidate}, nil
		})
		if err != nil {
			failureCause := autoFinalizeFailureCause(err)
			breaker, breakerErr := recordAutoFinalizeCircuitBreakerFailure(ctx, runner, repositoryID, runID, fmt.Sprint(row["workflow_job_id"]), failureCause, err, policy)
			if breakerErr != nil {
				return nil, breakerErr
			}
			skipped = append(skipped, map[string]any{
				"workflow_job_id": row["workflow_job_id"],
				"job_id":          jobID,
				"reason":          "live auto-finalize failed: " + err.Error(),
				"cause":           failureCause,
				"cause_detail":    errorDetail(err),
				"circuit_breaker": breaker,
			})
			continue
		}
		if result["eligible"] == true {
			finalized = append(finalized, result["finalized"].(map[string]any))
		} else {
			skipped = append(skipped, asMap(result["skip"]))
		}
	}
	freshPolicy, err := readAutoFinalizePolicy(ctx, runner, repositoryID, runID, envelope, mtimeGraceSeconds)
	if err != nil {
		freshPolicy = policy
	}
	result := map[string]any{
		"run_id":                    runID,
		"dry_run":                   false,
		"policy":                    freshPolicy,
		"lane_finalization_summary": autoFinalizeLaneFinalizationSummary(len(finalized), 0, 0),
		"eligible_count":            0,
		"eligible":                  []map[string]any{},
		"finalized_count":           len(finalized),
		"finalized":                 finalized,
		"skipped_count":             len(skipped),
		"skipped":                   skipped,
	}
	// #274 (RFC 0125 P1-3): the live path finalizes only live-lease candidates,
	// so blocked / lease-released jobs are equally invisible here. Surface them
	// as explanatory skip records (read-only probe; never finalized).
	if _, err := withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		return nil, appendAutoFinalizeBlockedJobSkips(ctx, tx, repositoryID, repoRoot, runID, stringParam(envelope, "job_id"), result)
	}); err != nil {
		return nil, err
	}
	if resetResult != nil {
		result["circuit_breaker_reset"] = resetResult
	}
	return result, nil
}

func autoFinalizeDryRun(envelope rpc.Envelope) (bool, error) {
	value, exists := envelope.Params["dry_run"]
	if !exists || value == nil {
		return true, nil
	}
	typed, ok := value.(bool)
	if !ok {
		return false, rpc.NewError("schema_invalid", "dry_run must be a boolean when present", nil)
	}
	return typed, nil
}

func autoFinalizeMtimeGraceSeconds(envelope rpc.Envelope) (int, error) {
	value := intParam(envelope, "mtime_grace_seconds", defaultAutoFinalizeMtimeGraceSeconds)
	if value < 0 {
		return 0, rpc.NewError("invalid_transition", "mtime_grace_seconds must be non-negative", nil)
	}
	return value, nil
}

func readAutoFinalizePolicy(ctx context.Context, runner db.Runner, repositoryID, runID string, envelope rpc.Envelope, mtimeGraceSeconds int) (map[string]any, error) {
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, false)
		if err != nil {
			return nil, err
		}
		snapshot, err := rowByID(ctx, tx, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(run["workflow_snapshot_id"]), false)
		if err != nil {
			return nil, err
		}
		workflow := asMap(snapshot["workflow_json"])
		policy := any(nil)
		if recovery := asMap(workflow["recovery"]); len(recovery) > 0 {
			policy = recovery["auto_finalize"]
		}
		if policy == nil {
			policy = workflow["auto_finalize"]
		}
		enabled, configured, optOut := autoFinalizePolicyState(policy)
		circuitBreaker, err := autoFinalizeCircuitBreakerPolicy(ctx, tx, repositoryID, runID, policy)
		if err != nil {
			return nil, err
		}
		force := boolParam(envelope, "force")
		return map[string]any{
			"workflow_enabled":           enabled,
			"workflow_configured":        configured,
			"workflow_opt_out":           optOut,
			"force":                      force,
			"live_allowed":               enabled || force,
			"global_default_mode":        "live",
			"default_live_gate":          autoFinalizeDefaultLiveGate(),
			"mtime_grace_seconds":        mtimeGraceSeconds,
			"allow_no_process_execution": boolParam(envelope, "allow_no_process_execution"),
			"circuit_breaker":            circuitBreaker,
		}, nil
	})
}

func autoFinalizePolicyState(policy any) (enabled bool, configured bool, optOut bool) {
	configured = policy != nil
	optOut = false
	switch typed := policy.(type) {
	case bool:
		optOut = !typed
	case map[string]any:
		optOut = typed["enabled"] == false
	default:
		if policyMap := asMap(policy); len(policyMap) > 0 {
			optOut = policyMap["enabled"] == false
		}
	}
	return !optOut, configured, optOut
}

func autoFinalizeDefaultLiveGate() map[string]any {
	return map[string]any{
		"decision_id":                      "D125",
		"status":                           "satisfied",
		"required_live_successes":          3,
		"required_lane_shapes":             2,
		"max_contested_audit_chain_events": 0,
		"evidence_artifact_kind":           "auto_finalize_gate_evidence",
		"live_default_enabled":             true,
		"enabled_by_decision_id":           "D133",
	}
}

func autoFinalizeEmptyResult(runID string, dryRun bool, reason string, policy map[string]any) map[string]any {
	return map[string]any{
		"run_id":                    runID,
		"dry_run":                   dryRun,
		"policy":                    policy,
		"lane_finalization_summary": autoFinalizeLaneFinalizationSummary(0, 0, 0),
		"eligible_count":            0,
		"eligible":                  []map[string]any{},
		"finalized_count":           0,
		"finalized":                 []map[string]any{},
		"skipped_count":             1,
		"skipped":                   []map[string]any{{"reason": reason}},
	}
}

func autoFinalizeCircuitBreakerPolicy(ctx context.Context, runner any, repositoryID, runID string, policy any) (map[string]any, error) {
	config := autoFinalizeCircuitBreakerConfig(policy)
	openBreakers, err := openAutoFinalizeCircuitBreakers(ctx, runner, repositoryID, runID, "")
	if err != nil {
		return nil, err
	}
	state := "closed"
	if len(openBreakers) > 0 {
		state = "open"
	}
	return map[string]any{
		"enabled":         config["enabled"],
		"max_consecutive": config["max_consecutive"],
		"window_seconds":  config["window_seconds"],
		"state":           state,
		"open":            openBreakers,
	}, nil
}

func autoFinalizeCircuitBreakerConfig(policy any) map[string]any {
	enabled := true
	maxConsecutive := defaultAutoFinalizeBreakerMax
	windowSeconds := defaultAutoFinalizeBreakerWindow
	if typed, ok := policy.(map[string]any); ok {
		switch breaker := typed["circuit_breaker"].(type) {
		case bool:
			enabled = breaker
		case map[string]any:
			if value, ok := breaker["enabled"].(bool); ok {
				enabled = value
			}
			maxConsecutive = positiveIntFromAny(breaker["max_consecutive"], defaultAutoFinalizeBreakerMax)
			windowSeconds = positiveIntFromAny(breaker["window_seconds"], defaultAutoFinalizeBreakerWindow)
		}
	}
	return map[string]any{
		"enabled":         enabled,
		"max_consecutive": maxConsecutive,
		"window_seconds":  windowSeconds,
	}
}

func openAutoFinalizeCircuitBreakerForJob(ctx context.Context, runner any, repositoryID, runID, workflowJobID string) (map[string]any, error) {
	rows, err := openAutoFinalizeCircuitBreakers(ctx, runner, repositoryID, runID, workflowJobID)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func openAutoFinalizeCircuitBreakers(ctx context.Context, runner any, repositoryID, runID, workflowJobID string) ([]map[string]any, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT workflow_job_id, cause, consecutive_failures, opened_at,
		       open_until, last_failure_at, last_error
		  FROM striatumd.auto_finalize_circuit_breakers
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND open_until > $3
		   AND ($4 = '' OR workflow_job_id = $4)
		 ORDER BY open_until DESC, workflow_job_id, cause`, repositoryID, runID, time.Now().UTC(), workflowJobID)
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for _, row := range rows {
		result = append(result, serializeAutoFinalizeCircuitBreaker(row, "open"))
	}
	return result, nil
}

func autoFinalizeCircuitBreakerSkip(row map[string]any, breaker map[string]any) map[string]any {
	reason := "auto-finalize circuit breaker is open"
	if openUntil := fmt.Sprint(breaker["open_until"]); openUntil != "" && openUntil != "<nil>" {
		reason += " until " + openUntil
	}
	return map[string]any{
		"workflow_job_id": row["workflow_job_id"],
		"job_id":          row["job_id"],
		"reason":          reason,
		"cause":           autoFinalizeCauseCircuitBreakerOpen,
		"cause_detail":    fmt.Sprint(breaker["cause"]),
		"circuit_breaker": breaker,
	}
}

func recordAutoFinalizeCircuitBreakerFailure(ctx context.Context, runner db.Runner, repositoryID, runID, workflowJobID, cause string, failure error, policy map[string]any) (map[string]any, error) {
	breakerPolicy := asMap(policy["circuit_breaker"])
	if breakerPolicy["enabled"] == false {
		return map[string]any{"enabled": false, "state": "disabled", "cause": cause}, nil
	}
	maxConsecutive := positiveIntFromAny(breakerPolicy["max_consecutive"], defaultAutoFinalizeBreakerMax)
	windowSeconds := positiveIntFromAny(breakerPolicy["window_seconds"], defaultAutoFinalizeBreakerWindow)
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		now := time.Now().UTC().Truncate(time.Second)
		cutoff := now.Add(-time.Duration(windowSeconds) * time.Second)
		row, err := oneRow(ctx, tx, `
			SELECT workflow_job_id, cause, consecutive_failures, opened_at,
			       open_until, last_failure_at, last_error
			  FROM striatumd.auto_finalize_circuit_breakers
			 WHERE repository_id = $1
			   AND run_id = $2
			   AND workflow_job_id = $3
			   AND cause = $4
			 FOR UPDATE`, repositoryID, runID, workflowJobID, cause)
		consecutive := 1
		var openedAt any
		if err == nil {
			if lastFailureAt, ok := timeFromAny(row["last_failure_at"]); ok && !lastFailureAt.Before(cutoff) {
				consecutive = intFromAny(row["consecutive_failures"], 0) + 1
			}
			openedAt = row["opened_at"]
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		var openUntil any
		if consecutive >= maxConsecutive {
			if openedAt == nil {
				openedAt = now
			}
			openUntil = now.Add(time.Duration(windowSeconds) * time.Second)
		}
		message := errorDetail(failure)
		stored, err := oneRow(ctx, tx, `
			INSERT INTO striatumd.auto_finalize_circuit_breakers (
			  repository_id, run_id, workflow_job_id, cause,
			  consecutive_failures, opened_at, open_until,
			  last_failure_at, last_error, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (repository_id, run_id, workflow_job_id, cause)
			DO UPDATE SET
			  consecutive_failures = EXCLUDED.consecutive_failures,
			  opened_at = EXCLUDED.opened_at,
			  open_until = EXCLUDED.open_until,
			  last_failure_at = EXCLUDED.last_failure_at,
			  last_error = EXCLUDED.last_error,
			  updated_at = EXCLUDED.updated_at
			RETURNING workflow_job_id, cause, consecutive_failures,
			          opened_at, open_until, last_failure_at, last_error`,
			repositoryID, runID, workflowJobID, cause, consecutive, openedAt, openUntil, now, message, now)
		if err != nil {
			return nil, err
		}
		state := "closed"
		if stored["open_until"] != nil {
			state = "open"
			if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.auto_finalize.circuit_breaker_open", nil, nil, nil, nil, nil, map[string]any{
				"workflow_job_id":      workflowJobID,
				"cause":                cause,
				"consecutive_failures": consecutive,
				"open_until":           timeString(openUntil),
				"window_seconds":       windowSeconds,
				"max_consecutive":      maxConsecutive,
				"last_error":           message,
			}); err != nil {
				return nil, err
			}
		}
		return serializeAutoFinalizeCircuitBreaker(stored, state), nil
	})
}

func resetAutoFinalizeCircuitBreakers(ctx context.Context, runner db.Runner, repositoryID, runID, jobID string) (map[string]any, error) {
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		var rows []map[string]any
		var err error
		if jobID == "" {
			rows, err = queryRows(ctx, tx, `
				DELETE FROM striatumd.auto_finalize_circuit_breakers
				 WHERE repository_id = $1 AND run_id = $2
				RETURNING workflow_job_id, cause`, repositoryID, runID)
		} else {
			rows, err = queryRows(ctx, tx, `
				DELETE FROM striatumd.auto_finalize_circuit_breakers cb
				 USING striatumd.jobs j
				 WHERE cb.repository_id = $1
				   AND cb.run_id = $2
				   AND j.repository_id = cb.repository_id
				   AND j.run_id = cb.run_id
				   AND j.workflow_job_id = cb.workflow_job_id
				   AND j.job_id = $3
				RETURNING cb.workflow_job_id, cb.cause`, repositoryID, runID, jobID)
		}
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			if _, err := appendEvent(ctx, tx, repositoryID, runID, "recovery.auto_finalize.circuit_breaker_reset", nil, nil, nil, nil, nil, map[string]any{
				"source":  "operator_reset",
				"job_id":  nullable(jobID),
				"cleared": rows,
			}); err != nil {
				return nil, err
			}
		}
		return map[string]any{"cleared_count": len(rows), "cleared": rows}, nil
	})
}

func clearAutoFinalizeCircuitBreakersForJob(ctx context.Context, runner any, repositoryID, runID, workflowJobID string) ([]map[string]any, error) {
	rows, err := queryRows(ctx, runner, `
		DELETE FROM striatumd.auto_finalize_circuit_breakers
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND workflow_job_id = $3
		RETURNING workflow_job_id, cause`, repositoryID, runID, workflowJobID)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		if _, err := appendEvent(ctx, runner, repositoryID, runID, "recovery.auto_finalize.circuit_breaker_reset", nil, nil, nil, nil, nil, map[string]any{
			"source":          "successful_auto_finalize",
			"workflow_job_id": workflowJobID,
			"cleared":         rows,
		}); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func serializeAutoFinalizeCircuitBreaker(row map[string]any, state string) map[string]any {
	return map[string]any{
		"state":                state,
		"workflow_job_id":      row["workflow_job_id"],
		"cause":                row["cause"],
		"consecutive_failures": intFromAny(row["consecutive_failures"], 0),
		"opened_at":            timeString(row["opened_at"]),
		"open_until":           timeString(row["open_until"]),
		"last_failure_at":      timeString(row["last_failure_at"]),
		"last_error":           row["last_error"],
	}
}

func autoFinalizeFailureCause(err error) string {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "publish") || strings.Contains(text, "artifact"):
		return autoFinalizeCauseFinalizePublishFailed
	case strings.Contains(text, "verdict") || strings.Contains(text, "review"):
		return autoFinalizeCauseFinalizeVerdictFailed
	case strings.Contains(text, "complete") || strings.Contains(text, "job"):
		return autoFinalizeCauseFinalizeCompleteFailed
	default:
		return autoFinalizeCauseFinalizeUnexpected
	}
}

func positiveIntFromAny(value any, fallback int) int {
	parsed := intFromAny(value, fallback)
	if parsed <= 0 {
		return fallback
	}
	return parsed
}

func intFromAny(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func timeFromAny(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case string:
		parsed, err := time.Parse(time.RFC3339, typed)
		return parsed, err == nil
	default:
		return time.Time{}, false
	}
}

func timeString(value any) any {
	if value == nil {
		return nil
	}
	if typed, ok := timeFromAny(value); ok {
		return typed.UTC().Format(time.RFC3339)
	}
	return fmt.Sprint(value)
}

func errorDetail(err error) string {
	text := strings.TrimSpace(err.Error())
	if len(text) > 240 {
		return text[:240]
	}
	return text
}

func autoFinalizeCandidateRows(ctx context.Context, runner any, repositoryID, runID, jobID string) ([]map[string]any, error) {
	return queryRows(ctx, runner, `
		SELECT j.*, l.lease_id, l.owner_session_id, l.expires_at,
		       l.state AS lease_state, s.state AS session_state,
		       qm.message_id, qm.state AS message_state,
		       qm.created_at AS message_created_at
		  FROM striatumd.jobs j
		  JOIN striatumd.leases l
		    ON l.repository_id = j.repository_id
		   AND l.lease_id = j.current_lease_id
		  JOIN striatumd.sessions s
		    ON s.repository_id = j.repository_id
		   AND s.session_id = l.owner_session_id
		  LEFT JOIN striatumd.queue_messages qm
		    ON qm.repository_id = j.repository_id
		   AND qm.message_id = j.current_message_id
		 WHERE j.repository_id = $1
		   AND j.run_id = $2
		   AND j.state IN ('claimed', 'running')
		   AND l.state = 'active'
		   AND l.expires_at >= $3::timestamptz
		   AND s.state = 'active'
		   AND (qm.message_id IS NULL OR qm.state IN ('claimed', 'acked'))
		   AND ($4 = '' OR j.job_id = $4)
		 ORDER BY j.workflow_job_id`, repositoryID, runID, nowString(), jobID)
}

// autoFinalizeBlockedJobRows returns the run's jobs that auto-finalize cannot
// even consider because they are no longer running with an active lease — a
// blocked or human-waiting job (lease released, current_lease_id NULL). The
// live-lease candidate join in autoFinalizeCandidateRows filters these out
// BEFORE eligible_count is computed (#274 root cause), so they must be surfaced
// separately to explain an otherwise-silent eligible_count:0.
func autoFinalizeBlockedJobRows(ctx context.Context, runner any, repositoryID, runID, jobID string) ([]map[string]any, error) {
	return queryRows(ctx, runner, `
		SELECT j.*
		  FROM striatumd.jobs j
		 WHERE j.repository_id = $1
		   AND j.run_id = $2
		   AND j.state IN ('blocked', 'waiting_human')
		   AND ($3 = '' OR j.job_id = $3)
		 ORDER BY j.workflow_job_id`, repositoryID, runID, jobID)
}

// openBlockerForJob returns the most recent open blocker row for a job, or nil.
func openBlockerForJob(ctx context.Context, runner any, repositoryID, jobID string) (map[string]any, error) {
	row, err := oneRow(ctx, runner, `
		SELECT blocker_id, severity, blocker_kind, description, state, created_at
		  FROM striatumd.blockers
		 WHERE repository_id = $1
		   AND job_id = $2
		   AND state = 'open'
		 ORDER BY created_at DESC, blocker_id DESC
		 LIMIT 1`, repositoryID, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// appendAutoFinalizeBlockedJobSkips enriches an auto-finalize result with an
// EXPLICIT skip record per blocked / lease-released job of the run (#274,
// RFC 0125 P1-3). Each record names the job and explains WHY auto-finalize did
// not consider it, and — when the cause is an undurable published artifact —
// points the operator at `recovery reseal`. eligible_count and the set of
// finalized jobs are left untouched; this only grows skipped/skipped_count.
//
// The result map is mutated in place. Probing is read-only (the same
// publishedArtifactDurabilityProblems probe used by the worktree-durability
// completion guard); it never mutates state and never finalizes a job.
func appendAutoFinalizeBlockedJobSkips(ctx context.Context, runner any, repositoryID, repoRoot, runID, jobID string, result map[string]any) error {
	rows, err := autoFinalizeBlockedJobRows(ctx, runner, repositoryID, runID, jobID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	skipped := asList(result["skipped"])
	for _, job := range rows {
		skip, err := autoFinalizeBlockedJobSkip(ctx, runner, repositoryID, repoRoot, job)
		if err != nil {
			return err
		}
		skipped = append(skipped, skip)
	}
	result["skipped"] = skipped
	result["skipped_count"] = len(skipped)
	return nil
}

// autoFinalizeBlockedJobSkip builds the skip record for a single blocked /
// lease-released job. It runs the read-only published-artifact durability probe
// against the job's active worktree (when the job is a repo-write job with one);
// if any published artifact is not durable in the worktree HEAD the record gets
// the published_artifact_not_durable reason plus a `recovery reseal` hint,
// otherwise it falls back to the blocker condition with an inspect-blocker hint.
func autoFinalizeBlockedJobSkip(ctx context.Context, runner any, repositoryID, repoRoot string, job map[string]any) (map[string]any, error) {
	jobID := fmt.Sprint(job["job_id"])
	skip := map[string]any{
		"workflow_job_id": job["workflow_job_id"],
		"job_id":          jobID,
		"job_state":       job["state"],
	}

	blocker, err := openBlockerForJob(ctx, runner, repositoryID, jobID)
	if err != nil {
		return nil, err
	}
	if blocker != nil {
		skip["blocker_id"] = blocker["blocker_id"]
		skip["blocker_kind"] = blocker["blocker_kind"]
		skip["blocker_severity"] = blocker["severity"]
	}

	durabilityProblems, err := autoFinalizeBlockedJobDurabilityProblems(ctx, runner, repositoryID, repoRoot, job)
	if err != nil {
		return nil, err
	}

	if len(durabilityProblems) > 0 {
		artifactIDs := make([]string, 0, len(durabilityProblems))
		for _, p := range durabilityProblems {
			artifactIDs = append(artifactIDs, p.ArtifactID)
		}
		skip["reason"] = autoFinalizeReasonPublishedArtifactNotDurable
		skip["artifacts"] = publishedArtifactDurabilityProblemMaps(durabilityProblems)
		skip["explanation"] = fmt.Sprintf(
			"job is %s; auto-finalize only considers running jobs with an active lease. Its published artifact %s is not durable in HEAD; use `recovery reseal` after committing the body, or inspect the blocker.",
			fmt.Sprint(job["state"]), strings.Join(artifactIDs, ", "))
		skip["remediation"] = "recovery reseal"
		return skip, nil
	}

	skip["reason"] = autoFinalizeReasonBlocked
	blockerKind := ""
	if blocker != nil {
		blockerKind = strings.TrimSpace(fmt.Sprint(nullable(blocker["blocker_kind"])))
	}
	if blockerKind != "" && blockerKind != "<nil>" {
		skip["explanation"] = fmt.Sprintf(
			"job is %s on blocker %q; auto-finalize only considers running jobs with an active lease. Inspect the blocker and resolve it before retrying.",
			fmt.Sprint(job["state"]), blockerKind)
	} else {
		skip["explanation"] = fmt.Sprintf(
			"job is %s; auto-finalize only considers running jobs with an active lease. Inspect the blocker and resolve it before retrying.",
			fmt.Sprint(job["state"]))
	}
	skip["remediation"] = "inspect_blocker"
	return skip, nil
}

// autoFinalizeBlockedJobDurabilityProblems runs the read-only published-artifact
// durability probe against a blocked job's active worktree. It returns no
// problems (and no error) when the job is not a repo-write per-job-worktree job
// or has no active worktree — the durability hazard simply does not apply.
func autoFinalizeBlockedJobDurabilityProblems(ctx context.Context, runner any, repositoryID, repoRoot string, job map[string]any) ([]publishedArtifactDurabilityProblem, error) {
	if !isRepoWrite(job) {
		return nil, nil
	}
	required, worktree, err := worktreeRequirementForJob(ctx, runner, repositoryID, job)
	if err != nil {
		return nil, err
	}
	if !required || worktree == nil {
		return nil, nil
	}
	root := strings.TrimSpace(repoRoot)
	if root == "" || root == "<nil>" {
		root, err = activeRepositoryRoot(ctx, runner, repositoryID)
		if err != nil {
			return nil, err
		}
	}
	return publishedArtifactDurabilityProblems(ctx, runner, artifactDurabilityCheck{
		RepositoryID: repositoryID,
		RepoRoot:     root,
		Job:          job,
		Worktree:     worktree,
	})
}

func evaluateAutoFinalizeRows(ctx context.Context, runner any, repositoryID, repoRoot, runID string, rows []map[string]any, mtimeGraceSeconds int, allowNoProcessExecution bool, policy map[string]any) (map[string]any, error) {
	eligible := []map[string]any{}
	skipped := []map[string]any{}
	for _, row := range rows {
		openBreaker, err := openAutoFinalizeCircuitBreakerForJob(ctx, runner, repositoryID, runID, fmt.Sprint(row["workflow_job_id"]))
		if err != nil {
			return nil, err
		}
		if openBreaker != nil {
			skipped = append(skipped, autoFinalizeCircuitBreakerSkip(row, openBreaker))
			continue
		}
		evaluation := evaluateAutoFinalizeCandidate(ctx, runner, repositoryID, repoRoot, row, mtimeGraceSeconds, allowNoProcessExecution)
		if evaluation["eligible"] == true {
			eligible = append(eligible, evaluation["candidate"].(map[string]any))
		} else {
			skipped = append(skipped, asMap(evaluation["skip"]))
		}
	}
	return map[string]any{
		"run_id":                    runID,
		"dry_run":                   true,
		"policy":                    policy,
		"lane_finalization_summary": autoFinalizeLaneFinalizationSummary(len(eligible), 0, 0),
		"eligible_count":            len(eligible),
		"eligible":                  eligible,
		"finalized_count":           0,
		"finalized":                 []map[string]any{},
		"skipped_count":             len(skipped),
		"skipped":                   skipped,
	}, nil
}

func autoFinalizeLaneFinalizationSummary(autoFromArtifact, manualPublish, pending int) map[string]any {
	return map[string]any{
		"auto_from_artifact": autoFromArtifact,
		"manual_publish":     manualPublish,
		"pending":            pending,
	}
}

func evaluateAutoFinalizeCandidate(ctx context.Context, runner any, repositoryID, repoRoot string, row map[string]any, mtimeGraceSeconds int, allowNoProcessExecution bool) map[string]any {
	jobID := fmt.Sprint(row["job_id"])
	sessionID := fmt.Sprint(row["owner_session_id"])
	leaseID := fmt.Sprint(row["lease_id"])
	messageID := ""
	if nullable(row["message_id"]) != nil {
		messageID = fmt.Sprint(row["message_id"])
	}
	base := map[string]any{
		"workflow_job_id": row["workflow_job_id"],
		"job_id":          jobID,
		"session_id":      sessionID,
		"lease_id":        leaseID,
		"message_id":      nullable(messageID),
	}
	expected := []map[string]any{}
	for _, item := range asList(row["expected_artifacts_json"]) {
		declared := asMap(item)
		if declared["required"] == false {
			continue
		}
		expected = append(expected, declared)
	}
	if len(expected) == 0 {
		return autoFinalizeNotEligible(base, "no required expected_artifacts declared")
	}
	if messageID == "" {
		return autoFinalizeNotEligible(base, "no active queue message is attached")
	}
	expectedByline, err := expectedAuthorLine(ctx, runner, repositoryID, row, sessionID)
	if err != nil {
		return autoFinalizeNotEligible(base, "could not derive expected byline: "+err.Error())
	}

	artifacts := []map[string]any{}
	refusals := []map[string]any{}
	for _, declared := range expected {
		result := evaluateAutoFinalizeArtifact(ctx, runner, repositoryID, repoRoot, row, sessionID, expectedByline, declared, mtimeGraceSeconds, allowNoProcessExecution)
		if result["ok"] == true {
			artifacts = append(artifacts, result["artifact"].(map[string]any))
		} else {
			refusals = append(refusals, asMap(result["refusal"]))
		}
	}
	if len(refusals) > 0 {
		skip := cloneMap(base)
		skip["reason"] = "expected_artifact validation refused"
		skip["artifacts"] = refusals
		return map[string]any{"eligible": false, "skip": skip}
	}
	candidate := cloneMap(base)
	candidate["lane_finalization"] = "auto_from_artifact"
	candidate["message_state"] = row["message_state"]
	candidate["job_state"] = row["state"]
	candidate["job_type"] = row["job_type"]
	candidate["expected_byline"] = expectedByline
	candidate["artifacts"] = artifacts
	return map[string]any{"eligible": true, "candidate": candidate}
}

func evaluateAutoFinalizeArtifact(ctx context.Context, runner any, repositoryID, repoRoot string, job map[string]any, sessionID, expectedByline string, declared map[string]any, mtimeGraceSeconds int, allowNoProcessExecution bool) map[string]any {
	pathText, _ := declared["path"].(string)
	kind, _ := declared["kind"].(string)
	logicalName, _ := declared["logical_name"].(string)
	if pathText == "" {
		return autoFinalizeArtifactRefusal(declared, "expected_artifact path is missing", nil)
	}
	if kind == "" || !allowedArtifactKinds[kind] {
		return autoFinalizeArtifactRefusal(declared, "expected_artifact kind is invalid", nil)
	}
	if kind == "transcript" {
		return autoFinalizeArtifactRefusal(declared, "transcript artifacts are not auto-finalized", nil)
	}
	if logicalName == "" {
		return autoFinalizeArtifactRefusal(declared, "expected_artifact logical_name is missing", nil)
	}
	if !pathAllowed(repoRoot, pathText, asMap(job["write_scope_json"])) {
		return autoFinalizeArtifactRefusal(declared, "artifact path is outside the job write scope", nil)
	}
	path, err := repoRelativePath(repoRoot, pathText, false)
	if err != nil {
		return autoFinalizeArtifactRefusal(declared, err.Error(), nil)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return autoFinalizeArtifactRefusal(declared, "expected artifact file does not exist", nil)
	}
	stable := autoFinalizeMtimeStable(info, mtimeGraceSeconds)
	if stable["stable"] != true {
		return autoFinalizeArtifactRefusal(declared, "expected artifact file mtime is inside the grace period", map[string]any{"age_seconds": stable["age_seconds"]})
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return autoFinalizeArtifactRefusal(declared, "could not read expected artifact: "+err.Error(), nil)
	}
	frontMatter, err := autoFinalizeRequiredFrontMatter(kind, path, payload)
	if err != nil {
		return autoFinalizeArtifactRefusal(declared, err.Error(), nil)
	}
	if err := validateArtifactFrontMatter(kind, path, payload); err != nil {
		return autoFinalizeArtifactRefusal(declared, err.Error(), nil)
	}
	if err := validateAutoFinalizeRequiredByline(payload, expectedByline); err != nil {
		return autoFinalizeArtifactRefusal(declared, err.Error(), nil)
	}
	if reason, err := autoFinalizeLaneEvidenceRefusal(ctx, runner, repositoryID, sessionID, expectedByline, allowNoProcessExecution); err != nil {
		return autoFinalizeArtifactRefusal(declared, err.Error(), nil)
	} else if reason != "" {
		return autoFinalizeArtifactRefusal(declared, reason, nil)
	}
	// The current work message's created_at is the current attempt's re-enqueue
	// boundary (RFC 0095 Phase 2 / #65 P2). A satisfying artifact older than it
	// belongs to a prior attempt and must not auto-finalize the re-opened job.
	existing, err := autoFinalizeExistingArtifactStatus(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), fmt.Sprint(job["job_id"]), logicalName, pathText, payload, job["message_created_at"], jobAttemptValue(job["attempt"]))
	if err != nil {
		return autoFinalizeArtifactRefusal(declared, err.Error(), nil)
	}
	if existing["status"] == "conflict" || existing["status"] == "stale_attempt" {
		return autoFinalizeArtifactRefusal(declared, fmt.Sprint(existing["reason"]), nil)
	}
	return map[string]any{
		"ok": true,
		"artifact": map[string]any{
			"path":              pathText,
			"disk_path":         path,
			"kind":              kind,
			"logical_name":      logicalName,
			"sha256":            sha256Hex(payload),
			"size_bytes":        len(payload),
			"mtime_age_seconds": stable["age_seconds"],
			"publish_status":    existing["status"],
			"artifact_id":       existing["artifact_id"],
			"front_matter":      frontMatter,
		},
	}
}

func autoFinalizeNotEligible(base map[string]any, reason string) map[string]any {
	skip := cloneMap(base)
	skip["reason"] = reason
	return map[string]any{"eligible": false, "skip": skip}
}

func autoFinalizeArtifactRefusal(declared map[string]any, reason string, extra map[string]any) map[string]any {
	refusal := map[string]any{
		"path":         declared["path"],
		"kind":         declared["kind"],
		"logical_name": declared["logical_name"],
		"reason":       reason,
	}
	for key, value := range extra {
		refusal[key] = value
	}
	return map[string]any{"ok": false, "refusal": refusal}
}

func autoFinalizeMtimeStable(info os.FileInfo, graceSeconds int) map[string]any {
	age := time.Since(info.ModTime()).Seconds()
	if age < 0 {
		age = 0
	}
	return map[string]any{
		"stable":      age >= float64(graceSeconds),
		"age_seconds": float64(int(age*1000)) / 1000,
	}
}

func validateAutoFinalizeRequiredByline(payload []byte, expectedByline string) error {
	if !utf8.Valid(payload) {
		return fmt.Errorf("markdown artifact must be UTF-8")
	}
	lines := markdownTitleBlockAuthorLines(string(payload))
	if len(lines) == 0 {
		return fmt.Errorf("markdown artifact author line is required for auto-finalize")
	}
	for _, line := range lines {
		if canonicalBylineForm(line) == expectedByline {
			return nil
		}
	}
	return fmt.Errorf("markdown artifact author line must match expected work packet author line")
}

func autoFinalizeRequiredFrontMatter(kind, path string, payload []byte) (map[string]any, error) {
	if _, ok := frontMatterSchemas[kind]; !ok {
		return nil, nil
	}
	if !isMarkdownPath(path) {
		return nil, fmt.Errorf("%s artifact must be Markdown to auto-finalize front matter", kind)
	}
	if !utf8.Valid(payload) {
		return nil, fmt.Errorf("%s artifact must be UTF-8 to validate front matter", kind)
	}
	block, ok := frontMatterBlock(string(payload))
	if !ok {
		return nil, fmt.Errorf("%s artifact front matter is required for auto-finalize", kind)
	}
	parsed, err := parseFrontMatterBlock(block)
	if err != nil {
		return nil, err
	}
	if kind == "finding" {
		if _, ok := parsed["verdict_intent"].(string); !ok {
			return nil, fmt.Errorf("finding artifact front matter missing required field 'verdict_intent'")
		}
	}
	return parsed, nil
}

func autoFinalizeLaneEvidenceRefusal(ctx context.Context, runner any, repositoryID, sessionID, expectedByline string, allowNoProcessExecution bool) (string, error) {
	if strings.HasPrefix(expectedByline, "author: operator") || allowNoProcessExecution {
		return "", nil
	}
	found, err := existsRow(ctx, runner, `
		SELECT 1
		  FROM striatumd.process_executions
		 WHERE repository_id = $1
		   AND session_id = $2
		   AND state = 'exited'
		   AND exit_code = 0
		 LIMIT 1`, repositoryID, sessionID)
	if err != nil {
		return "", err
	}
	if !found {
		return "lane_evidence_missing: no clean process_executions row for the session", nil
	}
	return "", nil
}

func autoFinalizeExistingArtifactStatus(ctx context.Context, runner any, repositoryID, runID, jobID, logicalName, pathText string, payload []byte, attemptBoundary any, attempt int) (map[string]any, error) {
	digest := sha256Hex(payload)
	// RFC 0095 §1 / #84: artifacts are attempt-scoped. Restrict the satisfying
	// artifact lookup to the job's CURRENT attempt so a re-opened job is never
	// auto-finalized from a PRIOR attempt's row — the prior row simply does not
	// match this query, and the function reports `would_publish` (the fresh lane
	// must publish its own attempt-scoped artifact, or a manual complete fires).
	// The created_at boundary check below is retained as defense-in-depth for the
	// legacy/NULL-attempt path (Phase 2 / #65 P2).
	row, err := oneRow(ctx, runner, `
		SELECT artifact_id, repo_path, content_sha256, created_at
		  FROM striatumd.artifacts
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3
		   AND logical_name = $4 AND attempt = $5
		 LIMIT 1`, repositoryID, runID, jobID, logicalName, attempt)
	if err == nil {
		if fmt.Sprint(row["repo_path"]) == pathText && fmt.Sprint(row["content_sha256"]) == digest {
			// Phase 2 (#65 P2) defense-in-depth: even within the current attempt,
			// if a satisfying artifact predates the current attempt's re-enqueue
			// boundary it is the prior attempt's unchanged output (e.g. before the
			// precise attempt column existed) — refuse it so the fresh lane must
			// (re)publish before auto-finalize fires. A NULL boundary (no message
			// join) keeps the legacy behavior.
			if stale, reason := autoFinalizeArtifactPredatesAttempt(row["created_at"], attemptBoundary); stale {
				return map[string]any{"status": "stale_attempt", "reason": reason}, nil
			}
			return map[string]any{"status": "already_published", "artifact_id": row["artifact_id"]}, nil
		}
		return map[string]any{"status": "conflict", "reason": artifactLogicalNameConflictMessage}, nil
	}
	if err == pgx.ErrNoRows {
		return map[string]any{"status": "would_publish", "artifact_id": nil}, nil
	}
	return nil, err
}

// autoFinalizeArtifactPredatesAttempt reports whether a satisfying artifact's
// created_at is strictly before the current attempt's re-enqueue boundary (the
// current work message's created_at). When it is, the artifact belongs to a
// prior attempt and must not be used to auto-finalize the re-opened job.
func autoFinalizeArtifactPredatesAttempt(artifactCreatedAt, attemptBoundary any) (bool, string) {
	boundary, ok := asTime(attemptBoundary)
	if !ok {
		return false, ""
	}
	created, ok := asTime(artifactCreatedAt)
	if !ok {
		return false, ""
	}
	if created.Before(boundary) {
		return true, fmt.Sprintf(
			"satisfying artifact predates the current attempt's re-enqueue (artifact created_at %s < message enqueued_at %s); a re-opened job must republish before auto-finalize",
			created.UTC().Format(time.RFC3339), boundary.UTC().Format(time.RFC3339))
	}
	return false, ""
}

func finalizeAutoFinalizeCandidate(ctx context.Context, runner any, repositoryID string, candidate map[string]any, allowNoProcessExecution bool) (map[string]any, error) {
	sessionID := fmt.Sprint(candidate["session_id"])
	jobID := fmt.Sprint(candidate["job_id"])
	leaseID := fmt.Sprint(candidate["lease_id"])
	messageID := fmt.Sprint(candidate["message_id"])
	if candidate["message_state"] == "claimed" {
		if err := ackInline(ctx, runner, repositoryID, sessionID, messageID, leaseID); err != nil {
			return nil, err
		}
	}

	published := []map[string]any{}
	for _, item := range asList(candidate["artifacts"]) {
		artifact := asMap(item)
		result, err := publishArtifact(ctx, runner, repositoryID, sessionID, jobID, leaseID, fmt.Sprint(artifact["kind"]), fmt.Sprint(artifact["logical_name"]), fmt.Sprint(artifact["path"]))
		if err != nil {
			return nil, err
		}
		artifactID := fmt.Sprint(result["artifact_id"])
		runID, err := autoFinalizeRunID(ctx, runner, repositoryID, jobID)
		if err != nil {
			return nil, err
		}
		payload := map[string]any{
			"path":         artifact["path"],
			"kind":         artifact["kind"],
			"logical_name": artifact["logical_name"],
			"sha256":       valueOr(result["sha256"], artifact["sha256"]),
			"validation": map[string]any{
				"byline":            candidate["expected_byline"],
				"mtime_age_seconds": artifact["mtime_age_seconds"],
				"source":            "expected_artifact_file",
			},
			"rationale": autoFinalizeSummary,
		}
		if allowNoProcessExecution {
			payload["override_rationale"] = noProcessAutoFinalizeRationale
		}
		if _, err := appendEvent(ctx, runner, repositoryID, runID, "artifact.auto_finalized", sessionID, jobID, nil, artifactID, leaseID, payload); err != nil {
			return nil, err
		}
		published = append(published, map[string]any{
			"path":         artifact["path"],
			"kind":         artifact["kind"],
			"logical_name": artifact["logical_name"],
			"artifact_id":  artifactID,
			"status":       result["status"],
		})
	}

	runID, err := autoFinalizeRunID(ctx, runner, repositoryID, jobID)
	if err != nil {
		return nil, err
	}
	if _, err := appendEvent(ctx, runner, repositoryID, runID, "job.auto_finalized", sessionID, jobID, nullable(messageID), nil, leaseID, map[string]any{
		"artifact_count":    len(published),
		"rationale":         autoFinalizeSummary,
		"lane_finalization": "auto_from_artifact",
		"validation": map[string]any{
			"byline": candidate["expected_byline"],
			"source": "expected_artifact_file",
		},
	}); err != nil {
		return nil, err
	}

	var completeResult map[string]any
	switch fmt.Sprint(candidate["job_type"]) {
	case "review", "phase_synthesis":
		verdictArtifact, err := autoFinalizeVerdictArtifact(asList(candidate["artifacts"]))
		if err != nil {
			return nil, err
		}
		verdict := fmt.Sprint(asMap(verdictArtifact["front_matter"])["verdict_intent"])
		artifactID, err := autoFinalizePublishedArtifactID(published, fmt.Sprint(verdictArtifact["logical_name"]))
		if err != nil {
			return nil, err
		}
		// RFC 0118 P0-2 (mechanism 3): an auto-finalized verdict is a daemon
		// recovery action, not an attested lane clear — declare the override
		// basis so the frozen stamp (P0-1) can never read back as a clean
		// neutral verdict and the run can never complete as lanes_attested.
		completeResult, err = recordVerdict(ctx, runner, repositoryID, sessionID, jobID, leaseID, verdict, artifactID, autoFinalizeSummary, recordVerdictOptions{
			ProvenanceOverrideBasis: "daemon_auto_finalized_from_artifact",
		})
		if err != nil {
			return nil, err
		}
	default:
		var err error
		completeResult, err = completeAutoFinalizedJob(ctx, runner, repositoryID, jobID, sessionID, leaseID, messageID)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"workflow_job_id":   candidate["workflow_job_id"],
		"job_id":            jobID,
		"session_id":        sessionID,
		"lane_finalization": "auto_from_artifact",
		"summary":           autoFinalizeSummary,
		"artifacts":         published,
		"complete":          completeResult,
	}, nil
}

func autoFinalizeRunID(ctx context.Context, runner any, repositoryID, jobID string) (any, error) {
	job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, false)
	if err != nil {
		return nil, err
	}
	return job["run_id"], nil
}

func autoFinalizeVerdictArtifact(artifacts []any) (map[string]any, error) {
	for _, item := range artifacts {
		artifact := asMap(item)
		if artifact["kind"] != "finding" {
			continue
		}
		frontMatter := asMap(artifact["front_matter"])
		if _, ok := frontMatter["verdict_intent"].(string); ok {
			return artifact, nil
		}
	}
	return nil, rpc.NewError("invalid_transition", "review auto-finalize requires a finding expected_artifact with verdict_intent front matter", nil)
}

func autoFinalizePublishedArtifactID(published []map[string]any, logicalName string) (any, error) {
	for _, artifact := range published {
		if fmt.Sprint(artifact["logical_name"]) == logicalName {
			return artifact["artifact_id"], nil
		}
	}
	return nil, rpc.NewError("invalid_transition", "review auto-finalize could not resolve findings artifact id", nil)
}

func completeAutoFinalizedJob(ctx context.Context, runner any, repositoryID, jobID, sessionID, leaseID, messageID string) (map[string]any, error) {
	job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		return nil, err
	}
	if fmt.Sprint(job["state"]) != "running" {
		return nil, rpc.NewError("invalid_transition", "job must be running before auto-finalize completion", nil)
	}
	if err := verifyRequiredArtifacts(ctx, runner, repositoryID, jobID); err != nil {
		return nil, err
	}
	// FMA-005 (#455): enforce the same per-job durability floor as
	// completeRecoveredJob. verifyRequiredArtifacts only covers the
	// required-artifact row-presence floor; the auto-finalize twin must also
	// run the per-job durability probe so it cannot seal a job `completed`
	// while a published (including non-required) repo-write artifact body is
	// non-durable outside the per-job worktree — the floor its recovery sibling
	// already enforces.
	if err := ensurePerJobPublishedArtifactsDurable(ctx, runner, repositoryID, job, "recovery.auto-finalize"); err != nil {
		return nil, err
	}
	now := nowString()
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return nil, fmt.Errorf("runner does not support exec")
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'completed', completed_at = $1, current_lease_id = NULL
		 WHERE repository_id = $2 AND job_id = $3`, now, repositoryID, jobID); err != nil {
		return nil, err
	}
	if messageID != "" && messageID != "<nil>" {
		if err := exec.Exec(ctx, `
			UPDATE striatumd.queue_messages
			   SET state = 'completed', completed_at = $1, updated_at = $2,
			       current_lease_id = NULL
			 WHERE repository_id = $3 AND message_id = $4`, now, now, repositoryID, messageID); err != nil {
			return nil, err
		}
	}
	if err := exec.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'released', released_at = COALESCE(released_at, $1),
		       release_reason = COALESCE(release_reason, 'auto_finalized')
		 WHERE repository_id = $2 AND lease_id = $3`, now, repositoryID, leaseID); err != nil {
		return nil, err
	}
	if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "job.completed", sessionID, jobID, nullable(messageID), nil, leaseID, map[string]any{
		"summary": autoFinalizeSummary,
	}); err != nil {
		return nil, err
	}
	// #304: resolve the completing job's open autonomous blockers so an
	// auto-finalized job does not leave a blocked-severity blocker dangling.
	if err := resolveAutonomousBlockersOnCompletion(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), jobID, sessionID, now); err != nil {
		return nil, err
	}
	if err := markJobTerminal(ctx, runner, repositoryID, fmt.Sprint(job["run_id"]), jobID); err != nil {
		return nil, err
	}
	if err := maybeEnqueueDownstream(ctx, runner, repositoryID, jobID); err != nil {
		return nil, err
	}
	if err := maybeCompleteRun(ctx, runner, repositoryID, fmt.Sprint(job["run_id"])); err != nil {
		return nil, err
	}
	return map[string]any{"status": "completed", "job_id": jobID}, nil
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func cloneMap(input map[string]any) map[string]any {
	output := map[string]any{}
	for key, value := range input {
		output[key] = value
	}
	return output
}
