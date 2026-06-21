package mutations

import (
	"context"
	"errors"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

func TestWorkflowHasAutoSpawnLane(t *testing.T) {
	cases := []struct {
		name     string
		workflow map[string]any
		want     bool
	}{
		{
			name:     "no lanes",
			workflow: map[string]any{"lanes": map[string]any{}},
			want:     false,
		},
		{
			name: "lane opts in",
			workflow: map[string]any{"lanes": map[string]any{
				"codex": map[string]any{"supervision": map[string]any{"auto_spawn": true}},
			}},
			want: true,
		},
		{
			name: "lane opts out explicitly over workflow default",
			workflow: map[string]any{
				"supervision": map[string]any{"auto_spawn": true},
				"lanes": map[string]any{
					"codex": map[string]any{"supervision": map[string]any{"auto_spawn": false}},
				},
			},
			want: false,
		},
		{
			name: "workflow-wide default applies to a lane with no override",
			workflow: map[string]any{
				"supervision": map[string]any{"auto_spawn": true},
				"lanes":       map[string]any{"codex": map[string]any{}},
			},
			want: true,
		},
		{
			name: "no supervision anywhere",
			workflow: map[string]any{"lanes": map[string]any{
				"codex": map[string]any{"capabilities": []any{"write"}},
			}},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workflowHasAutoSpawnLane(tc.workflow); got != tc.want {
				t.Fatalf("workflowHasAutoSpawnLane = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSpawnRunAsSpecDaemonUserMode(t *testing.T) {
	t.Setenv(supervisedLaneOSUserEnv, "")
	spec, err := spawnRunAsSpec()
	if err != nil {
		t.Fatalf("spawnRunAsSpec: %v", err)
	}
	if spec["mode"] != "daemon_user" || spec["run_as_user"] != "" {
		t.Fatalf("spec = %#v, want daemon_user mode", spec)
	}
}

func TestSpawnRunAsSpecResolvesLaneUser(t *testing.T) {
	t.Setenv(supervisedLaneOSUserEnv, "striatum-lane")
	restore := laneOSUserHome
	laneOSUserHome = func(name string) string {
		if name == "striatum-lane" {
			return "/home/striatum-lane"
		}
		return ""
	}
	defer func() { laneOSUserHome = restore }()
	restoreUser := currentOSUsername
	currentOSUsername = func() string { return "striatumd-daemon-user" }
	defer func() { currentOSUsername = restoreUser }()

	spec, err := spawnRunAsSpec()
	if err != nil {
		t.Fatalf("spawnRunAsSpec: %v", err)
	}
	if spec["mode"] != "lane_user" || spec["run_as_user"] != "striatum-lane" {
		t.Fatalf("spec = %#v, want lane_user striatum-lane", spec)
	}
}

// TestRunAsRefusalUnresolvedLaneUser: a configured lane OS user that cannot be
// resolved on the host is a loud refusal (RFC 0122 §4) — the scheduler must
// never spawn into a non-existent identity.
func TestRunAsRefusalUnresolvedLaneUser(t *testing.T) {
	t.Setenv(supervisedLaneOSUserEnv, "ghost-lane-user")
	restore := laneOSUserHome
	laneOSUserHome = func(string) string { return "" }
	defer func() { laneOSUserHome = restore }()

	_, err := spawnRunAsSpec()
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "spawn_run_as_unresolved" {
		t.Fatalf("spawnRunAsSpec err = %v, want spawn_run_as_unresolved", err)
	}
}

func TestSpawnCapabilityEnvelopeMatchesSessionBoundGrants(t *testing.T) {
	envelope := spawnCapabilityEnvelope()
	caps, ok := envelope["capabilities"].([]any)
	if !ok || len(caps) != len(sessionBoundCapabilities) {
		t.Fatalf("envelope capabilities = %#v, want %d entries", envelope["capabilities"], len(sessionBoundCapabilities))
	}
	for i, capability := range sessionBoundCapabilities {
		if caps[i] != string(capability) {
			t.Fatalf("capability[%d] = %v, want %s", i, caps[i], capability)
		}
	}
	if envelope["ttl_seconds"] != int(sessionBoundTokenTTL.Seconds()) {
		t.Fatalf("ttl_seconds = %v, want %d", envelope["ttl_seconds"], int(sessionBoundTokenTTL.Seconds()))
	}
}

// --- PG integration: grant capture at run.start ---

// seedAutoSpawnReadyRun seeds a repo + a ready run whose snapshot opts a lane
// into auto_spawn, plus one blocked root job. The grants table (migration 0027)
// is part of the runtime schema pgtest applies, so no owner bundle is needed.
func seedAutoSpawnReadyRun(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID string, autoSpawn bool) {
	t.Helper()
	laneSupervision := map[string]any{}
	if autoSpawn {
		laneSupervision = map[string]any{"auto_spawn": true}
	}
	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{
		"workflow_id": "wf",
		"roles":       map[string]any{"author": map[string]any{}},
		"lanes": map[string]any{"codex": map[string]any{
			"capabilities": []any{"write"},
			"supervision":  laneSupervision,
		}},
		"jobs": []any{map[string]any{"id": "author_draft", "type": "draft", "role_id": "author"}},
	})
	if err := runner.Exec(ctx, `
		UPDATE striatumd.runs SET state = 'ready'
		 WHERE repository_id = $1 AND run_id = $2`, repoID, runID); err != nil {
		t.Fatalf("mark run ready: %v", err)
	}
	insertWakeAuthorJob(t, ctx, runner, repoID, runID, "job_author", "author_draft", "blocked")
}

func ownerAuthEnv(repoID, clientID string, params map[string]any) (context.Context, rpc.Envelope) {
	ctx := rpc.WithAuthContext(context.Background(), rpc.AuthContext{
		ClientID:     clientID,
		RepositoryID: repoID,
	})
	return ctx, intgEnv(repoID, params)
}

func TestSpawnGrantCaptureOnAutoSpawnRunStart(t *testing.T) {
	runner := pgtest.Pool(t).Runner
	repoID := "repo_spawn_grant_capture"
	runID := "run_spawn_grant_capture"
	ctx := context.Background()
	seedAutoSpawnReadyRun(t, ctx, runner, repoID, runID, true)
	t.Setenv(supervisedLaneOSUserEnv, "") // daemon-user run-as resolves cleanly

	authCtx, env := ownerAuthEnv(repoID, "client_owner_op", map[string]any{"run_id": runID})
	if _, err := HandleRunStart(authCtx, runner, env); err != nil {
		t.Fatalf("run.start: %v", err)
	}

	grant, err := oneRow(ctx, runner, `
		SELECT owner_principal_id, run_as_spec, capability_envelope, revoked_at
		  FROM striatumd.spawn_authorization_grants
		 WHERE repository_id = $1 AND run_id = $2`, repoID, runID)
	if err != nil {
		t.Fatalf("read grant: %v", err)
	}
	if grant["owner_principal_id"] != "client_owner_op" {
		t.Fatalf("owner_principal_id = %v, want client_owner_op (C1 attestation parity)", grant["owner_principal_id"])
	}
	if grant["revoked_at"] != nil {
		t.Fatalf("freshly captured grant must be active, revoked_at = %v", grant["revoked_at"])
	}
	runAs := asMap(grant["run_as_spec"])
	if runAs["mode"] != "daemon_user" {
		t.Fatalf("run_as_spec = %#v, want daemon_user", runAs)
	}
	envelope := asMap(grant["capability_envelope"])
	caps := asList(envelope["capabilities"])
	if len(caps) != len(sessionBoundCapabilities) {
		t.Fatalf("capability_envelope = %#v, want %d caps", envelope, len(sessionBoundCapabilities))
	}
}

func TestSpawnGrantNotCapturedWithoutAutoSpawnLane(t *testing.T) {
	runner := pgtest.Pool(t).Runner
	repoID := "repo_no_spawn_grant"
	runID := "run_no_spawn_grant"
	ctx := context.Background()
	seedAutoSpawnReadyRun(t, ctx, runner, repoID, runID, false)

	authCtx, env := ownerAuthEnv(repoID, "client_owner_op", map[string]any{"run_id": runID})
	if _, err := HandleRunStart(authCtx, runner, env); err != nil {
		t.Fatalf("run.start: %v", err)
	}
	rows, err := queryRows(ctx, runner, `
		SELECT grant_id FROM striatumd.spawn_authorization_grants
		 WHERE repository_id = $1 AND run_id = $2`, repoID, runID)
	if err != nil {
		t.Fatalf("read grants: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("non-auto_spawn run captured %d grants, want 0", len(rows))
	}
}

// TestSpawnGrantCaptureRefusesAnonymousOwner: an auto_spawn run.start with no
// authenticated owner principal is refused — there is no identity to replay.
func TestSpawnGrantCaptureRefusesAnonymousOwner(t *testing.T) {
	runner := pgtest.Pool(t).Runner
	repoID := "repo_spawn_grant_anon"
	runID := "run_spawn_grant_anon"
	ctx := context.Background()
	seedAutoSpawnReadyRun(t, ctx, runner, repoID, runID, true)

	// No auth context → empty owner principal.
	_, err := HandleRunStart(ctx, runner, intgEnv(repoID, map[string]any{"run_id": runID}))
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "spawn_grant_no_owner_principal" {
		t.Fatalf("run.start err = %v, want spawn_grant_no_owner_principal", err)
	}
	// The refusal rolled back the whole start: the run stays ready, no grant.
	run, err := oneRow(ctx, runner, `SELECT state FROM striatumd.runs WHERE repository_id=$1 AND run_id=$2`, repoID, runID)
	if err != nil {
		t.Fatalf("read run: %v", err)
	}
	if run["state"] != "ready" {
		t.Fatalf("run state = %v, want ready (refused start rolls back)", run["state"])
	}
}

// TestRunAsRefusalAtRunStart: an auto_spawn run whose configured lane OS user
// cannot be resolved is refused at run.start, before any spawn is possible, and
// the start rolls back.
func TestRunAsRefusalAtRunStart(t *testing.T) {
	runner := pgtest.Pool(t).Runner
	repoID := "repo_spawn_runas_refusal"
	runID := "run_spawn_runas_refusal"
	ctx := context.Background()
	seedAutoSpawnReadyRun(t, ctx, runner, repoID, runID, true)
	t.Setenv(supervisedLaneOSUserEnv, "ghost-lane-user")
	restore := laneOSUserHome
	laneOSUserHome = func(string) string { return "" }
	defer func() { laneOSUserHome = restore }()

	authCtx, env := ownerAuthEnv(repoID, "client_owner_op", map[string]any{"run_id": runID})
	_, err := HandleRunStart(authCtx, runner, env)
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "spawn_run_as_unresolved" {
		t.Fatalf("run.start err = %v, want spawn_run_as_unresolved", err)
	}
	run, err := oneRow(ctx, runner, `SELECT state FROM striatumd.runs WHERE repository_id=$1 AND run_id=$2`, repoID, runID)
	if err != nil {
		t.Fatalf("read run: %v", err)
	}
	if run["state"] != "ready" {
		t.Fatalf("run state = %v, want ready (refused start rolls back)", run["state"])
	}
}

// TestSpawnGrantRevokedOnRunCancel: a terminal transition revokes the active
// grant (RFC 0122 §6), so the scheduler can never replay it past terminal.
func TestSpawnGrantRevokedOnRunCancel(t *testing.T) {
	runner := pgtest.Pool(t).Runner
	repoID := "repo_spawn_grant_revoke"
	runID := "run_spawn_grant_revoke"
	ctx := context.Background()
	seedAutoSpawnReadyRun(t, ctx, runner, repoID, runID, true)
	t.Setenv(supervisedLaneOSUserEnv, "")

	authCtx, env := ownerAuthEnv(repoID, "client_owner_op", map[string]any{"run_id": runID})
	if _, err := HandleRunStart(authCtx, runner, env); err != nil {
		t.Fatalf("run.start: %v", err)
	}
	if _, err := HandleRunCancel(ctx, runner, intgEnv(repoID, map[string]any{"run_id": runID, "reason": "test"})); err != nil {
		t.Fatalf("run.cancel: %v", err)
	}
	grant, err := oneRow(ctx, runner, `
		SELECT revoked_at, revoke_reason
		  FROM striatumd.spawn_authorization_grants
		 WHERE repository_id = $1 AND run_id = $2`, repoID, runID)
	if err != nil {
		t.Fatalf("read grant: %v", err)
	}
	if grant["revoked_at"] == nil {
		t.Fatalf("grant must be revoked after run cancel, got %#v", grant)
	}
}
