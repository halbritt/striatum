package mutations

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type inertRunner struct{}

func (inertRunner) Exec(context.Context, string, ...any) error {
	return errors.New("unexpected exec")
}

func (inertRunner) QueryRow(context.Context, string, ...any) db.Row {
	return fakeRow{err: pgx.ErrNoRows}
}

func (inertRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", nil
}

func (inertRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("unexpected tx")
}

type fakeRow struct {
	err error
}

func (r fakeRow) Scan(...any) error {
	return r.err
}

func TestRegisterInstallsInitialMutationHandlers(t *testing.T) {
	server := rpc.NewServer()
	Register(server, inertRunner{})

	for _, method := range []string{
		"session.register",
		"session.close",
		"session.report",
		"wake.wait",
		"work.claim_next",
		"claim_next",
		"work.ack",
		"ack",
		"work.heartbeat",
		"heartbeat",
		"work.release",
		"release",
		"work.send_message",
		"work.block",
		"block",
		"work.complete",
		"complete",
		"artifact.publish",
		"publish_artifact",
		"repo.write",
		"repo.patch_preview",
		"repo.patch_apply",
		"process.run",
		"worktree.create",
		"worktree.release",
		"worktree.anchor",
		"supervise.start",
		"supervise.send",
		"supervise.rebridge",
		"supervise.stop",
		"workflow.init",
		"workflow.generate",
		"workflow.upgrade",
		"workflow.accept_risk",
		"review.verdict",
		"verdict",
		"review.submit",
		"submit_review",
		"review.override",
		"run.prepare",
		"run.start",
		"run.pause",
		"run.resume",
		"run.cancel",
		"run.retry_job",
		"branch.confirm",
		"decision.record",
		"checkpoint.resolve",
		"recovery.stale_leases",
		"recovery.requeue_stale",
		"recovery.cancel_job",
		"recovery.process_reconcile",
		"recovery.resume",
		"recovery.sweep",
		"recovery.auto_publish_stale_artifacts",
		"recovery.auto_finalize",
		"recovery.invalidate_job",
		"supervise.report",
		"work.await_packet",
	} {
		handler, ok := server.Handlers[method]
		if !ok {
			t.Fatalf("%s was not registered", method)
		}
		_, err := handler(context.Background(), rpc.Envelope{
			SchemaVersion: rpc.SupportedEnvelopeVersion,
			RequestID:     "req_" + method,
			Method:        method,
			Params:        map[string]any{},
		})
		rpcErr := &rpc.Error{}
		if !errors.As(err, &rpcErr) || rpcErr.Code != "repo_not_registered" {
			t.Fatalf("%s handler did not run expected repo-scope validation: %v", method, err)
		}
	}
	if _, ok := server.Handlers["recovery.auto"]; ok {
		t.Fatal("retired recovery.auto alias must not remain registered")
	}
}

func TestRegisterExposesSuperviseRebridgeMethodMetadata(t *testing.T) {
	server := rpc.NewServer()
	Register(server, inertRunner{})

	if _, ok := server.Handlers["supervise.rebridge"]; !ok {
		t.Fatalf("supervise.rebridge handler was not registered")
	}
	entry, ok := rpc.MethodRegistry["supervise.rebridge"]
	if !ok {
		t.Fatalf("supervise.rebridge method metadata was not registered")
	}
	if entry.RequiredCapability == nil || *entry.RequiredCapability != rpc.CapabilityClaim || entry.RepositoryScopeMode != rpc.ScopeSingleRepo {
		t.Fatalf("supervise.rebridge metadata = %#v", entry)
	}
}

func TestSendMessageBodyDefaultsAndValidatesObjects(t *testing.T) {
	body, err := sendMessageBody(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 {
		t.Fatalf("default body = %#v", body)
	}

	body, err = sendMessageBody(map[string]any{"body_json": `{"summary":"working"}`})
	if err != nil {
		t.Fatal(err)
	}
	if body["summary"] != "working" {
		t.Fatalf("body summary = %#v", body["summary"])
	}

	_, err = sendMessageBody(map[string]any{"body_json": `[]`})
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "invalid_transition" {
		t.Fatalf("non-object body error = %v, want invalid_transition", err)
	}

	_, err = sendMessageBody(map[string]any{"body_json": `{`})
	rpcErr = nil
	if !errors.As(err, &rpcErr) || rpcErr.Code != "schema_invalid" {
		t.Fatalf("invalid JSON body error = %v, want schema_invalid", err)
	}
}

func TestRecoveryAutoFinalizeRequiresRunID(t *testing.T) {
	server := rpc.NewServer()
	Register(server, inertRunner{})
	handler := server.Handlers["recovery.auto_finalize"]
	if handler == nil {
		t.Fatal("recovery.auto_finalize was not registered")
	}
	_, err := handler(context.Background(), rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_recovery.auto_finalize",
		Method:        "recovery.auto_finalize",
		Params:        map[string]any{"repository_id": "repo_1"},
	})
	rpcErr := &rpc.Error{}
	if !errors.As(err, &rpcErr) {
		t.Fatalf("returned non-rpc error: %v", err)
	}
	if rpcErr.Code != "schema_invalid" {
		t.Fatalf("error code = %q", rpcErr.Code)
	}
	if rpcErr.Message != "recovery.auto_finalize requires run_id" {
		t.Fatalf("error message = %q", rpcErr.Message)
	}
}

func TestAutoFinalizeDryRunDefaultsToProjectionMode(t *testing.T) {
	dryRun, err := autoFinalizeDryRun(rpc.Envelope{Params: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !dryRun {
		t.Fatal("missing dry_run should default to true")
	}
	dryRun, err = autoFinalizeDryRun(rpc.Envelope{Params: map[string]any{"dry_run": false}})
	if err != nil {
		t.Fatal(err)
	}
	if dryRun {
		t.Fatal("explicit dry_run=false should request live mode")
	}
	_, err = autoFinalizeDryRun(rpc.Envelope{Params: map[string]any{"dry_run": "false"}})
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "schema_invalid" {
		t.Fatalf("non-boolean dry_run error = %v, want schema_invalid", err)
	}
}

func TestAutoFinalizePolicyStateDefaultsLiveWithExplicitOptOut(t *testing.T) {
	for _, tc := range []struct {
		name            string
		policy          any
		wantEnabled     bool
		wantConfigured  bool
		wantWorkflowOpt bool
	}{
		{"absent", nil, true, false, false},
		{"legacy-true", true, true, true, false},
		{"legacy-false", false, false, true, true},
		{"empty-map", map[string]any{}, true, true, false},
		{"enabled-true", map[string]any{"enabled": true}, true, true, false},
		{"enabled-false", map[string]any{"enabled": false}, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enabled, configured, optOut := autoFinalizePolicyState(tc.policy)
			if enabled != tc.wantEnabled || configured != tc.wantConfigured || optOut != tc.wantWorkflowOpt {
				t.Fatalf("policy state = enabled:%v configured:%v optOut:%v", enabled, configured, optOut)
			}
		})
	}

	gate := autoFinalizeDefaultLiveGate()
	if gate["status"] != "satisfied" || gate["live_default_enabled"] != true || gate["enabled_by_decision_id"] != "D133" {
		t.Fatalf("default live gate = %#v", gate)
	}
}

func TestAutoFinalizeResetCircuitBreakerRequiresLiveMode(t *testing.T) {
	server := rpc.NewServer()
	Register(server, inertRunner{})
	handler := server.Handlers["recovery.auto_finalize"]
	_, err := handler(context.Background(), rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_auto_finalize_reset",
		Method:        "recovery.auto_finalize",
		Params: map[string]any{
			"repository_id":              "repo_a",
			"run_id":                     "run_1",
			"dry_run":                    true,
			"reset_circuit_breaker":      true,
			"mtime_grace_seconds":        0,
			"allow_no_process_execution": true,
		},
	})
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "invalid_transition" {
		t.Fatalf("reset dry-run error = %v, want invalid_transition", err)
	}
	if rpcErr.Message != "--reset-circuit-breaker requires --live" {
		t.Fatalf("reset dry-run message = %q", rpcErr.Message)
	}
}

func TestAutoFinalizeCircuitBreakerConfigDefaultsAndOverrides(t *testing.T) {
	defaults := autoFinalizeCircuitBreakerConfig(nil)
	if defaults["enabled"] != true || defaults["max_consecutive"] != 3 || defaults["window_seconds"] != 600 {
		t.Fatalf("default breaker config = %#v", defaults)
	}
	overridden := autoFinalizeCircuitBreakerConfig(map[string]any{
		"circuit_breaker": map[string]any{
			"enabled":         false,
			"max_consecutive": float64(5),
			"window_seconds":  float64(90),
		},
	})
	if overridden["enabled"] != false || overridden["max_consecutive"] != 5 || overridden["window_seconds"] != 90 {
		t.Fatalf("overridden breaker config = %#v", overridden)
	}
}

func TestAutoFinalizeCircuitBreakerSkipShape(t *testing.T) {
	skip := autoFinalizeCircuitBreakerSkip(
		map[string]any{"workflow_job_id": "review", "job_id": "job_1"},
		map[string]any{"state": "open", "cause": autoFinalizeCauseFinalizePublishFailed, "open_until": "2026-05-22T00:10:00Z"},
	)
	if skip["cause"] != autoFinalizeCauseCircuitBreakerOpen {
		t.Fatalf("skip cause = %v", skip["cause"])
	}
	if skip["cause_detail"] != autoFinalizeCauseFinalizePublishFailed {
		t.Fatalf("skip cause_detail = %v", skip["cause_detail"])
	}
	if !strings.Contains(skip["reason"].(string), "2026-05-22T00:10:00Z") {
		t.Fatalf("skip reason = %v", skip["reason"])
	}
}

func TestAutoFinalizeFailureCauseClassifiesCommonFailureSites(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errors.New("artifact publish failed"), autoFinalizeCauseFinalizePublishFailed},
		{errors.New("record verdict failed"), autoFinalizeCauseFinalizeVerdictFailed},
		{errors.New("complete job failed"), autoFinalizeCauseFinalizeCompleteFailed},
		{errors.New("unexpected"), autoFinalizeCauseFinalizeUnexpected},
	} {
		if got := autoFinalizeFailureCause(tc.err); got != tc.want {
			t.Fatalf("cause for %q = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestAutoFinalizeFindingRequiresVerdictIntentFrontMatter(t *testing.T) {
	payload := []byte("---\nschema_version: \"striatum.finding.v1\"\nartifact_kind: \"finding\"\n---\n\nauthor: operator\n")
	_, err := autoFinalizeRequiredFrontMatter("finding", "finding.md", payload)
	if err == nil {
		t.Fatal("finding without verdict_intent should be refused")
	}
	if got := err.Error(); got != "finding artifact front matter missing required field 'verdict_intent'" {
		t.Fatalf("error = %q", got)
	}
}

func TestRegisterWithNilRunnerLeavesServerAlone(t *testing.T) {
	server := rpc.NewServer()
	Register(server, nil)
	if len(server.Handlers) != 0 {
		t.Fatalf("nil runner registered handlers: %v", server.Handlers)
	}
}

func TestRunPrepareUsesWorkflowAuthoringLoaderForRejectedPaths(t *testing.T) {
	parent := t.TempDir()
	repoRoot := filepath.Join(parent, "repo")
	outsideRoot := filepath.Join(parent, "outside")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRunPrepareWorkflow(t, filepath.Join(outsideRoot, "outside.json"))
	writeRunPrepareWorkflow(t, filepath.Join(repoRoot, "workflow.yaml"))

	for _, tc := range []struct {
		name      string
		workflow  string
		wantError string
	}{
		{
			name:      "traversal",
			workflow:  filepath.Join("..", "outside", "outside.json"),
			wantError: "workflow path must stay inside the repository",
		},
		{
			name:      "yaml",
			workflow:  "workflow.yaml",
			wantError: "workflow config must be JSON, not YAML",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &runPrepareLoaderTx{repoRoot: repoRoot}
			runner := &runPrepareLoaderRunner{tx: tx}
			_, err := HandleRunPrepare(context.Background(), runner, rpc.Envelope{
				SchemaVersion: rpc.SupportedEnvelopeVersion,
				RequestID:     "req_run_prepare_loader_" + tc.name,
				Method:        "run.prepare",
				Params: map[string]any{
					"repository_id": "repo_1",
					"workflow":      tc.workflow,
				},
			})
			var rpcErr *rpc.Error
			if !errors.As(err, &rpcErr) {
				t.Fatalf("run.prepare returned non-rpc error: %v", err)
			}
			if rpcErr.Code != "workflow_error" || !strings.Contains(rpcErr.Message, tc.wantError) {
				t.Fatalf("run.prepare error = %s %q, want workflow_error containing %q", rpcErr.Code, rpcErr.Message, tc.wantError)
			}
			if tx.execCount != 0 {
				t.Fatalf("run.prepare executed %d statements after rejected workflow load", tx.execCount)
			}
			if !tx.rolledBack || tx.committed {
				t.Fatalf("transaction committed=%v rolledBack=%v, want rollback only", tx.committed, tx.rolledBack)
			}
		})
	}
}

func TestRecoveryAutoPublishRequiresRunID(t *testing.T) {
	server := rpc.NewServer()
	Register(server, inertRunner{})

	method := "recovery.auto_publish_stale_artifacts"
	handler := server.Handlers[method]
	if handler == nil {
		t.Fatalf("%s was not registered", method)
	}
	_, err := handler(context.Background(), rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_" + method,
		Method:        method,
		Params:        map[string]any{"repository_id": "repo_1"},
	})
	rpcErr := &rpc.Error{}
	if !errors.As(err, &rpcErr) {
		t.Fatalf("%s returned non-rpc error: %v", method, err)
	}
	if rpcErr.Code != "schema_invalid" {
		t.Fatalf("%s error code = %q", method, rpcErr.Code)
	}
	if rpcErr.Message != "recovery.auto_publish_stale_artifacts requires run_id" {
		t.Fatalf("%s error message = %q", method, rpcErr.Message)
	}
}

func TestAutoPublishableArtifactsRequireMatchingBylineForEveryFile(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_autopub_byline"
	runID := "run_" + repoID
	jobID := "job_" + repoID
	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{
		"workflow_id": "wf",
		"roles":       map[string]any{"worker": map[string]any{}},
		"lanes":       map[string]any{"codex": map[string]any{}},
		"jobs":        []any{map[string]any{"id": "work", "type": "synthesis", "role_id": "worker"}},
	})

	repoRoot := t.TempDir()
	if err := os.WriteFile(repoRoot+"/notes.txt", []byte("author: worker-codex-001\n\nresult\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoRoot+"/wrong.md", []byte("author: reviewer-codex-001\n\nresult\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A real job row (attempt 1, no prior artifacts) so the #203 attempt-gate
	// query runs and finds nothing — the byline filter is what selects the files.
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, idempotency_key, expected_artifacts_json, write_scope_json,
		  lane_selector_json, created_at
		) VALUES ($1,$2,$3,'work',1,'running','worker','Work','synthesis','idem_'||$1,
		          '[]'::jsonb,'{}'::jsonb,'{"lane_id":"codex"}'::jsonb,NOW())`,
		repoID, jobID, runID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	job := map[string]any{
		"run_id":  runID,
		"job_id":  jobID,
		"attempt": 1,
		"expected_artifacts_json": []map[string]any{
			{"path": "notes.txt", "kind": "other", "logical_name": "notes", "required": true},
			{"path": "wrong.md", "kind": "other", "logical_name": "wrong", "required": true},
		},
	}
	got, _, err := autoPublishableArtifacts(ctx, runner, repoID, repoRoot, job, "sess_1", "author: worker-codex-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["path"] != "notes.txt" {
		t.Fatalf("publishable artifacts = %#v", got)
	}
}

func TestWorkflowDeclaresFreshReviewer(t *testing.T) {
	if workflowDeclaresFreshReviewer(map[string]any{"jobs": []any{map[string]any{"type": "draft"}}}) {
		t.Fatal("draft job should not trigger fresh reviewer policy")
	}
	if !workflowDeclaresFreshReviewer(map[string]any{"jobs": []any{map[string]any{"type": "review", "reviewer_context_policy": "fresh"}}}) {
		t.Fatal("fresh review policy was not detected")
	}
	if !workflowDeclaresFreshReviewer(map[string]any{"jobs": []any{map[string]any{"type": "review", "fresh_session_required": true}}}) {
		t.Fatal("fresh_session_required review was not detected")
	}
}

func TestValidateOperatorLabelRejectsReservedLane(t *testing.T) {
	_, err := validateOperatorLabel("codex", map[string]any{"lanes": map[string]any{"codex": map[string]any{}}})
	if err == nil {
		t.Fatal("expected reserved lane label to be rejected")
	}
	cleaned, err := validateOperatorLabel("  local.operator_1 ", map[string]any{"lanes": map[string]any{"codex": map[string]any{}}})
	if err != nil {
		t.Fatalf("valid label rejected: %v", err)
	}
	if cleaned != "local.operator_1" {
		t.Fatalf("cleaned label = %q", cleaned)
	}
}

func TestWorkflowPhaseEdgesMaterializeSynthesisFanIn(t *testing.T) {
	workflow := map[string]any{
		"schema_version": "striatum.workflow.v1.1",
		"workflow_id":    "wf-phases",
		"branch":         map[string]any{"mode": "confirm", "suggested_name": "wf/phases"},
		"roles":          map[string]any{"worker": map[string]any{}},
		"lanes":          map[string]any{"lane_a": map[string]any{}},
		"phases": []any{
			map[string]any{"id": "phase_1_design", "name": "Design"},
			map[string]any{"id": "phase_2_build", "name": "Build"},
		},
		"jobs": []any{
			phaseJob("design_a", "phase_1_design", "handoff"),
			phaseJob("synthesize_design", "phase_1_design", "phase_synthesis"),
			phaseJob("build_a", "phase_2_build", "handoff"),
			phaseJob("synthesize_build", "phase_2_build", "phase_synthesis"),
		},
		"edges": []any{
			map[string]any{"from": "synthesize_design", "to": "build_a", "on": "completed"},
		},
	}
	index, err := validateWorkflowForPrepare(workflow)
	if err != nil {
		t.Fatalf("workflow validation failed: %v", err)
	}
	pairs := edgeDependencyPairs(workflow, index, true)
	got := map[string]bool{}
	for _, pair := range pairs {
		got[pair.fromID+"->"+pair.toID] = true
	}
	for _, expected := range []string{
		"design_a->synthesize_design",
		"synthesize_design->build_a",
		"build_a->synthesize_build",
	} {
		if !got[expected] {
			t.Fatalf("missing materialized edge %s in %#v", expected, got)
		}
	}
}

func TestValidateWorkflowForPrepareAcceptsReferenceOnlyAugmentation(t *testing.T) {
	workflow := map[string]any{
		"schema_version": "striatum.workflow.v1",
		"workflow_id":    "wf-augmentation",
		"branch":         map[string]any{"mode": "confirm", "suggested_name": "wf/augmentation"},
		"roles":          map[string]any{"worker": map[string]any{}},
		"lanes":          map[string]any{"lane_a": map[string]any{}},
		"jobs": []any{map[string]any{
			"id":                 "draft",
			"type":               "handoff",
			"role_id":            "worker",
			"lane_id":            "lane_a",
			"expected_artifacts": []any{},
		}},
		"edges": []any{},
		"augmentation": map[string]any{
			"mode":                    "reference_only",
			"required":                false,
			"budget_per_packet_lines": 100,
			"sources": []any{
				map[string]any{"id": "local-corpus", "kind": "corpus_bundle", "path": "exports/corpus"},
			},
			"jobs": []any{"draft"},
		},
	}

	if _, err := validateWorkflowForPrepare(workflow); err != nil {
		t.Fatalf("workflow validation failed: %v", err)
	}
}

func TestValidateWorkflowForPrepareRefusesAutonomousSharedCheckoutRepoWrite(t *testing.T) {
	workflow := map[string]any{
		"schema_version": "striatum.workflow.v1",
		"workflow_id":    "wf-autonomous-shared-checkout",
		"branch":         map[string]any{"mode": "confirm", "suggested_name": "wf/autonomous"},
		"roles":          map[string]any{"worker": map[string]any{}},
		"lanes": map[string]any{"lane_a": map[string]any{
			"adapter_capabilities": map[string]any{"agent_loop": true},
		}},
		"jobs": []any{map[string]any{
			"id":      "build",
			"type":    "build",
			"role_id": "worker",
			"lane_id": "lane_a",
			"write_scope": map[string]any{
				"mode":       "repo_write",
				"repo_write": true,
			},
			"expected_artifacts": []any{},
		}},
		"edges": []any{},
	}

	_, err := validateWorkflowForPrepare(workflow)
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "workflow_error" {
		t.Fatalf("err = %v, want workflow_error", err)
	}
	if !strings.Contains(rpcErr.Message, "autonomous repo-write lanes must use per-job worktrees") {
		t.Fatalf("error message = %q", rpcErr.Message)
	}

	workflow["lanes"].(map[string]any)["lane_a"].(map[string]any)["worktree_isolation"] = "per_job"
	if _, err := validateWorkflowForPrepare(workflow); err != nil {
		t.Fatalf("per-job isolated workflow rejected: %v", err)
	}
}

func TestValidateWorkflowForPrepareRejectsRequiredAugmentation(t *testing.T) {
	workflow := map[string]any{
		"schema_version": "striatum.workflow.v1",
		"workflow_id":    "wf-augmentation",
		"branch":         map[string]any{"mode": "confirm", "suggested_name": "wf/augmentation"},
		"roles":          map[string]any{"worker": map[string]any{}},
		"lanes":          map[string]any{"lane_a": map[string]any{}},
		"jobs": []any{map[string]any{
			"id":                 "draft",
			"type":               "handoff",
			"role_id":            "worker",
			"lane_id":            "lane_a",
			"expected_artifacts": []any{},
		}},
		"edges": []any{},
		"augmentation": map[string]any{
			"mode":     "reference_only",
			"required": true,
			"sources": []any{
				map[string]any{"id": "local-corpus", "kind": "corpus_bundle", "path": "exports/corpus"},
			},
			"jobs": []any{"draft"},
		},
	}

	_, err := validateWorkflowForPrepare(workflow)
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "workflow_error" {
		t.Fatalf("err = %v, want workflow_error", err)
	}
	if !strings.Contains(rpcErr.Message, "augmentation.required") {
		t.Fatalf("error message = %q", rpcErr.Message)
	}
}

func phaseJob(id string, phaseID string, jobType string) map[string]any {
	return map[string]any{
		"id":       id,
		"type":     jobType,
		"role_id":  "worker",
		"lane_id":  "lane_a",
		"phase_id": phaseID,
		"write_scope": map[string]any{
			"allowed_paths": []any{"docs/"},
		},
		"expected_artifacts": []any{},
	}
}

type runPrepareLoaderRunner struct {
	tx *runPrepareLoaderTx
}

func (r *runPrepareLoaderRunner) Exec(context.Context, string, ...any) error {
	return errors.New("unexpected runner exec outside tx")
}

func (r *runPrepareLoaderRunner) QueryRow(context.Context, string, ...any) db.Row {
	return fakeRow{err: errors.New("unexpected runner query outside tx")}
}

func (r *runPrepareLoaderRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected runner scalar query outside tx")
}

func (r *runPrepareLoaderRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return r.tx, nil
}

type runPrepareLoaderTx struct {
	repoRoot   string
	execCount  int
	committed  bool
	rolledBack bool
}

func (tx *runPrepareLoaderTx) Exec(context.Context, string, ...any) error {
	tx.execCount++
	return errors.New("unexpected exec")
}

func (tx *runPrepareLoaderTx) QueryRow(context.Context, string, ...any) db.Row {
	return fakeRow{err: errors.New("unexpected row query")}
}

func (tx *runPrepareLoaderTx) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected scalar query")
}

func (tx *runPrepareLoaderTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if !strings.Contains(sql, "FROM striatumd.repositories") {
		return nil, errors.New("unexpected query: " + sql)
	}
	if len(args) != 2 || args[0] != "repo_1" || args[1] != "repo_1" {
		return runPrepareRowsFromMaps(nil), nil
	}
	return runPrepareRowsFromMaps([]map[string]any{{"repo_root": tx.repoRoot}}), nil
}

func (tx *runPrepareLoaderTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *runPrepareLoaderTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

type runPrepareRows struct {
	fields []string
	rows   [][]any
	index  int
}

func runPrepareRowsFromMaps(items []map[string]any) pgx.Rows {
	fields := []string{}
	if len(items) > 0 {
		for key := range items[0] {
			fields = append(fields, key)
		}
	}
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		row := make([]any, 0, len(fields))
		for _, field := range fields {
			row = append(row, item[field])
		}
		rows = append(rows, row)
	}
	return &runPrepareRows{fields: fields, rows: rows, index: -1}
}

func (r *runPrepareRows) Close() {}

func (r *runPrepareRows) Err() error { return nil }

func (r *runPrepareRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }

func (r *runPrepareRows) FieldDescriptions() []pgconn.FieldDescription {
	out := make([]pgconn.FieldDescription, 0, len(r.fields))
	for _, field := range r.fields {
		out = append(out, pgconn.FieldDescription{Name: field})
	}
	return out
}

func (r *runPrepareRows) Next() bool {
	r.index++
	return r.index < len(r.rows)
}

func (r *runPrepareRows) Scan(dest ...any) error {
	if len(dest) == 1 {
		if scanner, ok := dest[0].(pgx.RowScanner); ok {
			return scanner.ScanRow(r)
		}
	}
	values, err := r.Values()
	if err != nil {
		return err
	}
	for i := range dest {
		if i >= len(values) {
			break
		}
		switch target := dest[i].(type) {
		case *any:
			*target = values[i]
		case *string:
			if value, ok := values[i].(string); ok {
				*target = value
			}
		}
	}
	return nil
}

func (r *runPrepareRows) Values() ([]any, error) {
	if r.index < 0 || r.index >= len(r.rows) {
		return nil, errors.New("no current row")
	}
	return r.rows[r.index], nil
}

func (r *runPrepareRows) RawValues() [][]byte { return nil }

func (r *runPrepareRows) Conn() *pgx.Conn { return nil }

func writeRunPrepareWorkflow(t *testing.T, path string) {
	t.Helper()
	workflow := map[string]any{
		"schema_version":   "striatum.workflow.v1",
		"workflow_id":      "loader-test",
		"workflow_version": "1",
		"name":             "Loader Test",
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "test/loader"},
		"coordinator":      map[string]any{"role_id": "worker", "lane_id": "lane_a"},
		"lanes":            map[string]any{"lane_a": map[string]any{"command": []any{"true"}}},
		"roles":            map[string]any{"worker": map[string]any{"description": "worker"}},
		"context_docs":     []any{},
		"parallelism":      map[string]any{"max_active_jobs": float64(1)},
		"jobs": []any{
			map[string]any{
				"id":                 "job_a",
				"type":               "handoff",
				"role_id":            "worker",
				"lane_id":            "lane_a",
				"write_scope":        map[string]any{"allowed_paths": []any{"docs/"}},
				"expected_artifacts": []any{},
			},
		},
		"edges":  []any{},
		"cycles": []any{},
	}
	payload, err := json.Marshal(workflow)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
}
