package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/jackc/pgx/v5"
)

const recoverySchedulerPanicEnv = "STRIATUM_TEST_RECOVERY_SCHEDULER_PANIC"

type panicQueryRunner struct{}

func (panicQueryRunner) Exec(context.Context, string, ...any) error {
	return errors.New("unexpected Exec")
}

func (panicQueryRunner) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("recovery scheduler panic sentinel")
}

func (panicQueryRunner) QueryRow(context.Context, string, ...any) db.Row {
	panic("unexpected QueryRow")
}

func (panicQueryRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected QueryScalar")
}

func (panicQueryRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("unexpected BeginTx")
}

// TestRecoverySchedulerRecoversAndCancelsOnPanic verifies that a panic in the
// recovery scheduler goroutine is RECOVERED (not re-raised), logged with the
// sentinel and a stack trace, and triggers daemon context cancellation for a
// clean controlled restart (issue #451 / FMA-001).
//
// The inner subprocess branch exercises the scheduler directly: the panicQueryRunner
// panics on Query, the scheduler's defer/recover catches it, logs it, calls
// cancel(), and sends the error on done. The outer process checks that:
//   - the subprocess exits cleanly (no unhandled panic crash)
//   - the log output contains the panic sentinel and stack trace
//   - the log output does NOT contain a re-raised "panic: ..." crash line
func TestRecoverySchedulerRecoversAndCancelsOnPanic(t *testing.T) {
	if os.Getenv(recoverySchedulerPanicEnv) == "1" {
		log.SetFlags(0)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := startRecoveryScheduler(ctx, cancel, panicQueryRunner{}, 0.01, optionalIntFlag{Provided: true, Value: 1})
		select {
		case err := <-done:
			// New contract: the scheduler recovers the panic and returns an error on done.
			// Verify the error message encodes the sentinel.
			if err == nil {
				t.Fatal("scheduler returned nil error; want non-nil error encoding the panic value")
			}
			if !strings.Contains(err.Error(), "recovery scheduler panic sentinel") {
				t.Fatalf("scheduler error does not contain sentinel: %v", err)
			}
			// Verify the context was cancelled (cancel() was called by the recover handler).
			select {
			case <-ctx.Done():
				// expected
			default:
				t.Fatal("scheduler recover handler did not cancel the context")
			}
			// Success: recovered, error sent, context cancelled — os.Exit(0) via normal test return.
			return
		case <-time.After(2 * time.Second):
			t.Fatal("scheduler did not return within timeout after panic")
		}
	}

	// Outer process: run the inner test in a subprocess and verify the contract.
	outerCtx, outerCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer outerCancel()
	cmd := exec.CommandContext(outerCtx, os.Args[0], "-test.run=TestRecoverySchedulerRecoversAndCancelsOnPanic$")
	cmd.Env = append(os.Environ(), recoverySchedulerPanicEnv+"=1")
	output, err := cmd.CombinedOutput()
	if outerCtx.Err() != nil {
		t.Fatalf("child test timed out: %v\n%s", outerCtx.Err(), output)
	}
	// The subprocess must exit cleanly (panic recovered, not re-raised).
	if err != nil {
		t.Fatalf("child test exited with error (panic was re-raised or assertion failed); want clean exit. err=%v output:\n%s", err, output)
	}
	logText := string(output)
	// Verify the log contains the panic sentinel and stack trace from the recover handler.
	for _, want := range []string{
		"recovery scheduler goroutine panic recovered",
		"recovery scheduler panic sentinel",
		"runtime/debug.Stack",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("scheduler log output missing %q:\n%s", want, logText)
		}
	}
	// Verify the panic was NOT re-raised (no Go runtime "panic: ..." crash line).
	if strings.Contains(logText, "panic: recovery scheduler panic sentinel") {
		t.Fatalf("scheduler panic was re-raised (found 'panic: recovery scheduler panic sentinel'); want recover-only:\n%s", logText)
	}
	if strings.Contains(logText, "auto_spawn scheduler") {
		t.Fatalf("recovery scheduler panic output mentioned auto_spawn:\n%s", logText)
	}
}
