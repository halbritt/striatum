package mutations

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// TestBufferedPushPacketSurvivesDaemonRestart is the #456 (FMA-006) regression:
// a supervised_push packet written with no reader attached is buffered, and that
// buffer must survive a daemon restart and replay to the FIFO once a reader
// attaches — instead of being silently dropped with the in-memory pipeBuffers map.
//
// It drives the real writeSupervisorPayload delivery path against a migrated PG
// (so the 0038 supervisor_buffered_packets table exists), simulates a daemon
// restart by clearing the process-global in-memory buffer, then attaches a reader
// and re-delivers — asserting the originally buffered packet is replayed (not lost)
// and the durable rows are cleared afterward.
func TestBufferedPushPacketSurvivesDaemonRestart(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	dir := t.TempDir()
	pipePath := filepath.Join(dir, "stdin.pipe")
	if err := syscall.Mkfifo(pipePath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	resetPipeBuffer(pipePath)

	const (
		repoID = "repo-456"
		supID  = "sup-456"
	)
	firstPacket := []byte("packet-buffered-before-restart\n")

	// 1) No reader attached: writeSupervisorPayload buffers the packet AND persists
	//    it to the durable store so it survives a restart.
	res := deliverInTx(t, ctx, pool, repoID, supID, pipePath, firstPacket)
	if !res.Buffered {
		t.Fatal("expected the no-reader write to be buffered (no reader attached)")
	}
	if n := durableBufferedCount(t, ctx, pool, repoID, supID, pipePath); n != 1 {
		t.Fatalf("durable buffered rows = %d, want 1 (the buffered packet was not persisted)", n)
	}

	// 2) Simulate a daemon restart: the in-memory pipeBuffers map is gone. Without
	//    the durable store this is exactly where the packet was lost (#456).
	resetPipeBuffer(pipePath)

	// 3) A reader attaches (the lane finally opened its stdin) and the operator
	//    re-delivers a second packet. writeSupervisorPayload re-hydrates the
	//    in-memory queue from the durable store, so the FIRST (replayed) packet is
	//    flushed ahead of the second, and a non-buffered write clears the store.
	readerDone := startPipeReader(t, pipePath)
	secondPacket := []byte("packet-sent-after-restart\n")
	res = deliverInTx(t, ctx, pool, repoID, supID, pipePath, secondPacket)
	if res.Buffered {
		t.Fatal("expected delivery to a now-attached reader to be flushed, not buffered")
	}

	got := readerDone(t)
	want := string(firstPacket) + string(secondPacket)
	if got != want {
		t.Fatalf("reader saw %q, want %q (the buffered packet was not replayed across restart)", got, want)
	}

	// 4) The durable rows are cleared once a reader drained the queue, so a later
	//    restart does not re-deliver an already-delivered packet.
	if n := durableBufferedCount(t, ctx, pool, repoID, supID, pipePath); n != 0 {
		t.Fatalf("durable buffered rows = %d after delivery, want 0 (rows not cleared)", n)
	}
}

func resetPipeBuffer(pipePath string) {
	pipeBuffersMu.Lock()
	delete(pipeBuffers, pipePath)
	pipeBuffersMu.Unlock()
}

func deliverInTx(t *testing.T, ctx context.Context, pool *db.Pool, repoID, supID, pipePath string, payload []byte) supervisorDeliveryResult {
	t.Helper()
	tx, err := pool.Runner.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	res, err := writeSupervisorPayload(ctx, tx, repoID, supID, pipePath, payload)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("writeSupervisorPayload: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit delivery: %v", err)
	}
	return res
}

func durableBufferedCount(t *testing.T, ctx context.Context, pool *db.Pool, repoID, supID, pipePath string) int {
	t.Helper()
	value, err := pool.Runner.QueryScalar(ctx, `
SELECT count(*) FROM striatumd.supervisor_buffered_packets
 WHERE repository_id = $1 AND supervisor_id = $2 AND pipe_path = $3`, repoID, supID, pipePath)
	if err != nil {
		t.Fatalf("count durable buffered: %v", err)
	}
	n := 0
	for _, r := range value {
		n = n*10 + int(r-'0')
	}
	return n
}

// startPipeReader opens the FIFO non-blocking for reading and drains it in a
// goroutine. The persistent FIFO is never closed by the writer, so the reader
// accumulates bytes over a bounded quiescent window rather than waiting for EOF.
// The returned func blocks until the read window elapses (or a hard timeout) and
// returns what was read; opening the read end is what lets the no-reader buffered
// writes flush.
func startPipeReader(t *testing.T, pipePath string) func(t *testing.T) string {
	t.Helper()
	fd, err := syscall.Open(pipePath, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open pipe reader: %v", err)
	}
	reader := os.NewFile(uintptr(fd), pipePath)
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		acc := []byte{}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			n, _ := reader.Read(buf)
			if n > 0 {
				acc = append(acc, buf[:n]...)
				continue
			}
			// No data yet: EAGAIN (a writer is present but has not written), or
			// EOF (no writer has opened the FIFO yet, which a non-blocking O_RDONLY
			// reports as 0/EOF). Neither is end-of-stream for a persistent FIFO the
			// writer opens momentarily — back off and keep reading within the
			// quiescent window so the replayed + fresh packets are observed.
			time.Sleep(20 * time.Millisecond)
		}
		done <- string(acc)
	}()
	return func(t *testing.T) string {
		t.Helper()
		select {
		case got := <-done:
			_ = reader.Close()
			return got
		case <-time.After(5 * time.Second):
			_ = reader.Close()
			t.Fatal("pipe reader timed out")
			return ""
		}
	}
}
