package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
)

type noopDecayRunner struct{}

func (noopDecayRunner) Exec(context.Context, string, ...any) error {
	return errors.New("unexpected Exec")
}

func (noopDecayRunner) QueryRow(context.Context, string, ...any) db.Row {
	return nil
}

func (noopDecayRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected QueryScalar")
}

func (noopDecayRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("unexpected BeginTx")
}

func TestDefaultCullFoldTimeoutBelowSweepInterval(t *testing.T) {
	if DefaultCullFoldTimeout >= DefaultSweepInterval {
		t.Fatalf("DefaultCullFoldTimeout = %s, want below DefaultSweepInterval = %s", DefaultCullFoldTimeout, DefaultSweepInterval)
	}
}

func TestDecayTickSweepPanicRecoveredAndNoWrite(t *testing.T) {
	done := make(chan struct{})
	commits := 0
	sweep := &DecayTickSweep{
		Runner:  noopDecayRunner{},
		Timeout: time.Second,
		Logf:    func(string, ...any) {},
		scan: func(context.Context, db.Runner) ([]cullableChange, error) {
			panic("synthetic scanner panic")
		},
		commit: func(context.Context, db.Runner, []cullableChange) error {
			commits++
			return nil
		},
		onDone: func() { close(done) },
	}

	if _, err := sweep.SweepOnce(context.Background()); err != nil {
		t.Fatalf("SweepOnce returned error: %v", err)
	}
	waitDone(t, done)
	if commits != 0 {
		t.Fatalf("panic path reached commit %d time(s); want zero writes", commits)
	}
}

func TestDecayTickSweepOffPathDoesNotDeferNextRecoveryTick(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	controlRefreshes := runTwoFakeRecoveryTicks(t, nil, start)

	release := make(chan struct{})
	done := make(chan struct{})
	sweep := &DecayTickSweep{
		Runner:  noopDecayRunner{},
		Timeout: time.Hour,
		Logf:    func(string, ...any) {},
		scan: func(context.Context, db.Runner) ([]cullableChange, error) {
			<-release
			return nil, nil
		},
		commit: func(context.Context, db.Runner, []cullableChange) error { return nil },
		onDone: func() { close(done) },
	}
	defer func() {
		close(release)
		waitDone(t, done)
	}()

	cullRefreshes := runTwoFakeRecoveryTicks(t, func(ctx context.Context) {
		if _, err := sweep.SweepOnce(ctx); err != nil {
			t.Fatalf("cull fold returned error: %v", err)
		}
	}, start)

	if !cullRefreshes[1].Equal(controlRefreshes[1]) {
		t.Fatalf("tick-2 cursor refresh was deferred by cull fold: got %s, no-cull control %s", cullRefreshes[1], controlRefreshes[1])
	}
	if !sweep.inFlight.Load() {
		t.Fatalf("expected first cull scan to remain in flight while tick 2 still ran")
	}
}

func TestDecayTickSweepRefreshAssertionCatchesWaitPathJoin(t *testing.T) {
	start := time.Unix(200, 0).UTC()
	controlRefreshes := runTwoFakeRecoveryTicks(t, nil, start)

	badRefreshes := runTwoFakeRecoveryTicks(t, func(ctx context.Context) {
		clock := fakeClockFromContext(ctx)
		clock.advance(DefaultCullFoldTimeout)
	}, start)

	if badRefreshes[1].Equal(controlRefreshes[1]) {
		t.Fatalf("negative control did not move tick-2 refresh; refresh-not-deferred assertion would be vacuous")
	}
}

func TestDecayTickSweepLateReturnAfterTimeoutWritesNothing(t *testing.T) {
	release := make(chan struct{})
	done := make(chan struct{})
	commits := 0
	sweep := &DecayTickSweep{
		Runner:  noopDecayRunner{},
		Timeout: 10 * time.Millisecond,
		Logf:    func(string, ...any) {},
		scan: func(context.Context, db.Runner) ([]cullableChange, error) {
			<-release
			return []cullableChange{{
				key:   cullableKey{kind: "decision", ref: "decision:D267"},
				state: "nominated",
			}}, nil
		},
		commit: func(context.Context, db.Runner, []cullableChange) error {
			commits++
			return nil
		},
		onDone: func() { close(done) },
	}

	if _, err := sweep.SweepOnce(context.Background()); err != nil {
		t.Fatalf("SweepOnce returned error: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	close(release)
	waitDone(t, done)
	if commits != 0 {
		t.Fatalf("late-returning scan reached commit %d time(s); want zero writes", commits)
	}
}

func TestDecayTickSweepCooperativeScanStopsAtTimeout(t *testing.T) {
	done := make(chan struct{})
	commits := 0
	started := time.Now()
	sweep := &DecayTickSweep{
		Runner:  noopDecayRunner{},
		Timeout: 10 * time.Millisecond,
		Logf:    func(string, ...any) {},
		scan: func(ctx context.Context, _ db.Runner) ([]cullableChange, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
		commit: func(context.Context, db.Runner, []cullableChange) error {
			commits++
			return nil
		},
		onDone: func() { close(done) },
	}

	if _, err := sweep.SweepOnce(context.Background()); err != nil {
		t.Fatalf("SweepOnce returned error: %v", err)
	}
	waitDone(t, done)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cooperative scan did not stop promptly at timeout; elapsed=%s", elapsed)
	}
	if commits != 0 {
		t.Fatalf("timed-out cooperative scan reached commit %d time(s); want zero writes", commits)
	}
}

func TestDecayTickKnownSetCorpus(t *testing.T) {
	root := repoRootForTest(t)
	evaluations, err := scanRepositoryCullableEvaluations(context.Background(), root)
	if err != nil {
		t.Fatalf("scan repository cullable evaluations: %v", err)
	}

	assertNominated(t, evaluations, cullableKey{kind: "decision", ref: "decision:D267"})

	d081 := assertWithheld(t, evaluations, cullableKey{kind: "decision", ref: "decision:D081"})
	if d081.withheldBy != "clause_4" {
		t.Fatalf("D081 withheldBy = %q, want clause_4 (#618 documented audit citation)", d081.withheldBy)
	}
	if !hitPathContains(d081.countedHits, "docs/audits/STRIATUM_DECISION_RECORD_AUDIT_OPUS_4_8_2026-06-16.md") {
		t.Fatalf("D081 counted hits do not include the documented #618 audit citation: %#v", d081.countedHits)
	}

	for _, key := range []cullableKey{
		{kind: "rfc", ref: "rfc:0097"},
		{kind: "rfc", ref: "rfc:0027"},
		{kind: "rfc", ref: "rfc:0039"},
		{kind: "rfc", ref: "rfc:0041"},
		{kind: "rfc", ref: "rfc:0028"},
		{kind: "decision", ref: "decision:D084"},
		{kind: "decision", ref: "decision:D174"},
	} {
		assertWithheld(t, evaluations, key)
	}

	for key, evaluation := range evaluations {
		if key.kind == "branch" && evaluation.nominated {
			t.Fatalf("branch candidacy appeared in P0 known-set scan: %#v", evaluation)
		}
	}
}

func TestDecayTickStructuralStatusParsesBareAndBoldTitleBlocks(t *testing.T) {
	for name, lines := range map[string][]string{
		"bare": {
			"# RFC 0097",
			"Status: superseded by RFC 0116 / 0122 / 0124",
		},
		"bold": {
			"# RFC 0049",
			"**Status:** deprecated - overtaken by RFC 0088",
		},
	} {
		t.Run(name, func(t *testing.T) {
			status := structuralStatusFromHead(lines)
			if strings.HasPrefix(status, "**") {
				t.Fatalf("status retained bold marker: %q", status)
			}
			if status == "" {
				t.Fatalf("status was not parsed from %#v", lines)
			}
		})
	}
}

func TestDecayTickProtectedPathspecIsTreeLocal(t *testing.T) {
	protected := []string{
		"docs/records/_frozen/example.md",
		"docs/operator/workflows/example.md",
		"examples/example.md",
		"prompts/example.md",
		"docs/reference/spec.md",
		"README.md",
		"AGENTS.md",
	}
	for _, path := range protected {
		if !protectedCullPath(path) {
			t.Fatalf("%s classified unprotected; want protected", path)
		}
	}
	for _, path := range []string{
		"docs/rfcs/0097-full-workflow-run-orchestration.md",
		"docs/decisions/decision-log.md",
	} {
		if protectedCullPath(path) {
			t.Fatalf("%s classified protected; RFCs and decisions must stay eligible", path)
		}
	}
}

func TestDecayTickStaticSQLShape(t *testing.T) {
	body := readPackageFile(t, "decay_tick_sweep.go")
	for _, forbidden := range []string{
		"SELECT *",
		"striatumd.events",
		"striatumd.audit_log",
		"striatumd.verdicts",
		"superseded_by_decision_id",
		"doctorRecovery",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("decay tick sweep source contains forbidden surface %q", forbidden)
		}
	}
	if !strings.Contains(body, "SELECT kind, ref, candidacy_state") {
		t.Fatalf("decay tick sweep must read cullable_entity with explicit columns")
	}
}

type fakeClockKey struct{}

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func fakeClockFromContext(ctx context.Context) *fakeClock {
	clock, _ := ctx.Value(fakeClockKey{}).(*fakeClock)
	return clock
}

func runTwoFakeRecoveryTicks(t *testing.T, fold func(context.Context), start time.Time) []time.Time {
	t.Helper()
	maxSweeps := 2
	clock := &fakeClock{now: start}
	var refreshes []time.Time
	ctx := context.WithValue(context.Background(), fakeClockKey{}, clock)
	result, err := RunScheduler(ctx, SchedulerOptions{
		Interval:  DefaultSweepInterval,
		MaxSweeps: &maxSweeps,
		SweepOnce: func(ctx context.Context) (map[string]any, error) {
			refreshes = append(refreshes, clock.now)
			if fold != nil {
				fold(ctx)
			}
			return map[string]any{"status": "ok"}, nil
		},
		Wait: func(context.Context, time.Duration) bool {
			clock.advance(DefaultSweepInterval)
			return true
		},
	})
	if err != nil {
		t.Fatalf("RunScheduler returned error: %v", err)
	}
	if result.Sweeps != 2 {
		t.Fatalf("RunScheduler Sweeps = %d, want 2", result.Sweeps)
	}
	if len(refreshes) != 2 {
		t.Fatalf("refresh count = %d, want 2", len(refreshes))
	}
	return refreshes
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for decay sweep goroutine")
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod above %s", wd)
		}
		dir = parent
	}
}

func assertNominated(t *testing.T, evaluations map[cullableKey]cullableEvaluation, key cullableKey) {
	t.Helper()
	evaluation, ok := evaluations[key]
	if !ok {
		t.Fatalf("missing evaluation for %#v", key)
	}
	if !evaluation.nominated {
		t.Fatalf("%#v not nominated; withheldBy=%s hits=%#v successors=%#v", key, evaluation.withheldBy, evaluation.countedHits, evaluation.successors)
	}
}

func assertWithheld(t *testing.T, evaluations map[cullableKey]cullableEvaluation, key cullableKey) cullableEvaluation {
	t.Helper()
	evaluation, ok := evaluations[key]
	if !ok {
		t.Fatalf("missing evaluation for %#v", key)
	}
	if evaluation.nominated {
		t.Fatalf("%#v was nominated; want withheld", key)
	}
	return evaluation
}

func hitPathContains(hits []inboundHit, needle string) bool {
	for _, hit := range hits {
		if strings.Contains(hit.path, needle) {
			return true
		}
	}
	return false
}

func readPackageFile(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}
