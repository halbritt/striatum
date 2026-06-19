package mutations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5/pgconn"
)

type supervisorPipeNoReaderDeliveryError struct {
	supervisorID string
	metadata     map[string]any
	reason       string
}

func (e *supervisorPipeNoReaderDeliveryError) Error() string {
	return "supervisor delivery is degraded: " + e.reason
}

func (e *supervisorPipeNoReaderDeliveryError) Unwrap() error {
	return errSupervisorPipeNoReader
}

func ensureSupervisorFIFO(path string) error {
	if info, err := os.Stat(path); err == nil {
		if info.Mode()&os.ModeNamedPipe == 0 {
			return fmt.Errorf("stdin path exists but is not a FIFO: %s", path)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return supervisionMkfifo(path)
}

type supervisorDeliveryResult struct {
	BytesWritten          int
	StdinDelivery         string
	StdinClosedAfterWrite bool
	// Buffered is true when the payload could not be written to the FIFO because
	// no reader was attached, so it was queued in the process-global in-memory
	// pipeBuffers instead. A buffered write is NOT durably delivered: a daemon
	// restart drops the queue. The caller must record a buffered/degraded event,
	// not supervisor.packet_delivered (#358).
	Buffered bool
}

func writeSupervisorPayload(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID, pipePath string, payload []byte) (supervisorDeliveryResult, error) {
	metadata, err := pointerMetadata(ctx, runner, repositoryID, supervisorID)
	if err != nil {
		return supervisorDeliveryResult{}, err
	}
	stdinDelivery := metadataStdinDelivery(metadata)
	if stdinDelivery == stdinDeliveryOneShotEOF && metadata["stdin_delivery_consumed"] == true {
		return supervisorDeliveryResult{}, rpc.NewError("invalid_transition", "one-shot supervisor stdin has already been consumed", nil)
	}
	// #456 (FMA-006): a no-reader write buffers the payload in the process-global
	// in-memory pipeBuffers, which a daemon restart drops. Before every delivery
	// attempt, re-hydrate the in-memory queue for this pipe from the durable store
	// so any packet buffered by a prior (now-restarted) daemon is re-queued and the
	// reader-attach flush inside writeToPipe replays it the moment a reader exists.
	if err := hydratePipeBufferFromStore(ctx, runner, repositoryID, supervisorID, pipePath); err != nil {
		return supervisorDeliveryResult{}, err
	}
	bytesWritten, buffered, err := writeToPipe(ctx, pipePath, payload)
	if err != nil {
		if errors.Is(err, errSupervisorPipeNoReader) {
			return supervisorDeliveryResult{}, &supervisorPipeNoReaderDeliveryError{
				supervisorID: supervisorID,
				metadata:     metadata,
				reason:       "stdin_reader_missing",
			}
		}
		return supervisorDeliveryResult{}, err
	}
	// #456: keep the durable store in lockstep with the in-memory queue. A buffered
	// write persists the payload so it survives a restart; a non-buffered write means
	// a reader drained the whole queue (writeToPipe flushes PopAll() on a successful
	// open), so the persisted rows for this pipe are now delivered and can be cleared.
	if buffered {
		if err := persistBufferedPacket(ctx, runner, repositoryID, supervisorID, pipePath, payload); err != nil {
			return supervisorDeliveryResult{}, err
		}
	} else {
		if err := clearBufferedPackets(ctx, runner, repositoryID, supervisorID, pipePath); err != nil {
			return supervisorDeliveryResult{}, err
		}
	}
	closed := stdinDelivery == stdinDeliveryOneShotEOF
	// A buffered (no-reader) write was not flushed to the FIFO; do not consume a
	// one-shot stdin or remove the FIFO on a buffer-only write — the payload still
	// needs a reader to attach and drain it.
	if closed && !buffered {
		releaseOneShotFIFOHold(pipePath)
		_ = os.Remove(pipePath)
		if err := mergePointerMetadata(ctx, runner, repositoryID, supervisorID, map[string]any{"stdin_delivery_consumed": true}); err != nil {
			return supervisorDeliveryResult{}, err
		}
	}
	return supervisorDeliveryResult{
		BytesWritten:          bytesWritten,
		StdinDelivery:         stdinDelivery,
		StdinClosedAfterWrite: closed && !buffered,
		Buffered:              buffered,
	}, nil
}

type NamedPipeBuffer struct {
	mu       sync.Mutex
	queue    [][]byte
	degraded bool
}

func (b *NamedPipeBuffer) Push(payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.degraded {
		return fmt.Errorf("buffer is degraded")
	}
	if len(b.queue) >= supervisorBufferedPacketCap {
		b.degraded = true
		return fmt.Errorf("buffer overflow, degraded")
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	b.queue = append(b.queue, cp)
	return nil
}

func (b *NamedPipeBuffer) PopAll() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.queue
	b.queue = nil
	return q
}

func (b *NamedPipeBuffer) IsDegraded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.degraded
}

// SeedIfEmpty repopulates the queue from the durable store ONLY when the
// in-memory queue is currently empty (the post-restart case). When the queue
// already holds packets it is the live mirror of the store, so seeding is a
// no-op — this avoids double-queueing the same packet within one process while
// still replaying packets a prior daemon buffered before it restarted. It is a
// no-op on a degraded buffer (overflowed); a degraded buffer already refuses
// delivery, and the operator must re-send. Returns true when it seeded.
func (b *NamedPipeBuffer) SeedIfEmpty(packets [][]byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.degraded || len(b.queue) > 0 || len(packets) == 0 {
		return false
	}
	for _, pkt := range packets {
		cp := make([]byte, len(pkt))
		copy(cp, pkt)
		b.queue = append(b.queue, cp)
	}
	return true
}

var (
	pipeBuffersMu sync.Mutex
	pipeBuffers   = make(map[string]*NamedPipeBuffer)
)

var (
	oneShotFIFOHoldsMu sync.Mutex
	oneShotFIFOHolds   = make(map[string]*os.File)
)

func getPipeBuffer(pipePath string) *NamedPipeBuffer {
	pipeBuffersMu.Lock()
	defer pipeBuffersMu.Unlock()
	buf, ok := pipeBuffers[pipePath]
	if !ok {
		buf = &NamedPipeBuffer{}
		pipeBuffers[pipePath] = buf
	}
	return buf
}

func registerOneShotFIFOHold(pipePath string, file *os.File) {
	oneShotFIFOHoldsMu.Lock()
	defer oneShotFIFOHoldsMu.Unlock()
	if existing := oneShotFIFOHolds[pipePath]; existing != nil && existing != file {
		_ = existing.Close()
	}
	oneShotFIFOHolds[pipePath] = file
}

func releaseOneShotFIFOHold(pipePath string) {
	oneShotFIFOHoldsMu.Lock()
	file := oneShotFIFOHolds[pipePath]
	delete(oneShotFIFOHolds, pipePath)
	oneShotFIFOHoldsMu.Unlock()
	if file != nil {
		_ = file.Close()
	}
}

// writeToPipe writes payload to the supervisor FIFO. The returned bool reports
// whether the payload was BUFFERED (no reader attached, queued in the in-memory
// pipeBuffers) rather than flushed to the FIFO; a buffered write is not durably
// delivered (a daemon restart drops the queue), so the caller must not record it
// as a delivered packet (#358).
func writeToPipe(ctx context.Context, pipePath string, payload []byte) (int, bool, error) {
	buf := getPipeBuffer(pipePath)
	if buf.IsDegraded() {
		return 0, false, errSupervisorPipeNoReader
	}

	fd, err := syscall.Open(pipePath, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ENXIO) {
			if pushErr := buf.Push(payload); pushErr != nil {
				return 0, false, errSupervisorPipeNoReader
			}
			return len(payload), true, nil
		}
		return 0, false, err
	}
	file := os.NewFile(uintptr(fd), pipePath)
	defer func() { _ = file.Close() }()

	buffered := buf.PopAll()
	for _, pkt := range buffered {
		if _, err := writeAll(ctx, file, pkt); err != nil {
			return 0, false, err
		}
	}

	n, err := writeAll(ctx, file, payload)
	return n, false, err
}

func writeAll(ctx context.Context, file *os.File, payload []byte) (int, error) {
	total := 0
	for total < len(payload) {
		n, err := file.Write(payload[total:])
		if n > 0 {
			total += n
		}
		if err != nil {
			if errors.Is(err, syscall.EPIPE) {
				return total, rpc.NewError("invalid_transition", "supervisor pipe is broken; child has closed stdin", nil)
			}
			if errors.Is(err, syscall.EAGAIN) {
				select {
				case <-ctx.Done():
					return total, ctx.Err()
				case <-time.After(20 * time.Millisecond):
					continue
				}
			}
			return total, err
		}
		if n == 0 {
			return total, rpc.NewError("invalid_transition", "supervisor pipe write returned zero bytes", nil)
		}
	}
	return total, nil
}

func markPointerDeliveryDegraded(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID string, metadata map[string]any, reason string) error {
	updated := map[string]any{}
	for key, value := range metadata {
		updated[key] = value
	}
	delivery := map[string]any{
		"class":       "degraded",
		"healthy":     false,
		"reason":      reason,
		"observed_at": nowString(),
	}
	if tmux := asMap(updated["tmux"]); len(tmux) > 0 {
		tmux["delivery_liveness"] = delivery
		updated["tmux"] = tmux
	} else {
		updated["delivery_liveness"] = delivery
	}
	return mergePointerMetadata(ctx, runner, repositoryID, supervisorID, updated)
}

func metadataStdinDelivery(metadata map[string]any) string {
	value, _ := metadata["stdin_delivery"].(string)
	if value == stdinDeliveryOneShotEOF || value == stdinDeliveryPersistentFIFO {
		return value
	}
	return stdinDeliveryPersistentFIFO
}

// supervisorBufferedPacketCap bounds how many no-reader packets the durable store
// retains per pipe. It matches the in-memory NamedPipeBuffer cap (Push degrades at
// 10) so the durable store never accumulates unbounded: once the cap is reached the
// in-memory buffer is degraded and refuses further delivery (the operator must
// re-send), and persistBufferedPacket likewise refuses to grow the durable store
// past the cap.
const supervisorBufferedPacketCap = 10

// isUndefinedTableErr reports whether err is a PostgreSQL undefined_table (42P01).
// The #456 durable buffer is degrade-safe: a daemon deployed BEHIND the 0038
// migration has no supervisor_buffered_packets table, so the persist / hydrate /
// clear helpers swallow the missing-table error and fall back to the pre-#456
// in-memory-only behavior rather than failing the delivery.
func isUndefinedTableErr(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}

// hydratePipeBufferFromStore reloads any durably-buffered packets for this pipe
// into the in-memory NamedPipeBuffer, but only when that buffer is empty (the
// post-restart case). The reader-attach flush inside writeToPipe then replays the
// re-queued packets to the FIFO the moment a reader is present.
func hydratePipeBufferFromStore(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID, pipePath string) error {
	q, ok := runner.(queryer)
	if !ok {
		return nil
	}
	buf := getPipeBuffer(pipePath)
	if buf.IsDegraded() {
		return nil
	}
	rows, err := q.Query(ctx, `
SELECT payload
  FROM striatumd.supervisor_buffered_packets
 WHERE repository_id = $1 AND supervisor_id = $2 AND pipe_path = $3
 ORDER BY seq ASC`, repositoryID, supervisorID, pipePath)
	if err != nil {
		if isUndefinedTableErr(err) {
			return nil
		}
		return err
	}
	defer rows.Close()
	packets := [][]byte{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return err
		}
		packets = append(packets, payload)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	buf.SeedIfEmpty(packets)
	return nil
}

// persistBufferedPacket durably records a no-reader packet so it survives a daemon
// restart, keyed by (repository_id, supervisor_id, pipe_path) plus a monotone seq.
// It is bounded to supervisorBufferedPacketCap rows per pipe — past the cap it is a
// no-op (the in-memory buffer is already degraded at that depth, so nothing more is
// deliverable until the operator re-sends).
func persistBufferedPacket(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID, pipePath string, payload []byte) error {
	count, err := runner.QueryScalar(ctx, `
SELECT count(*)
  FROM striatumd.supervisor_buffered_packets
 WHERE repository_id = $1 AND supervisor_id = $2 AND pipe_path = $3`, repositoryID, supervisorID, pipePath)
	if err != nil {
		if isUndefinedTableErr(err) {
			return nil
		}
		return err
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(count)); n >= supervisorBufferedPacketCap {
		return nil
	}
	if err := runner.Exec(ctx, `
INSERT INTO striatumd.supervisor_buffered_packets
  (repository_id, supervisor_id, pipe_path, seq, payload)
VALUES (
  $1, $2, $3,
  COALESCE((SELECT max(seq) FROM striatumd.supervisor_buffered_packets
             WHERE repository_id = $1 AND supervisor_id = $2 AND pipe_path = $3), 0) + 1,
  $4
)`, repositoryID, supervisorID, pipePath, payload); err != nil {
		if isUndefinedTableErr(err) {
			return nil
		}
		return err
	}
	return nil
}

// clearBufferedPackets removes the durable rows for a pipe after a reader has
// drained the in-memory queue (a non-buffered write means writeToPipe flushed
// every queued packet to an attached reader), so a later restart does not replay
// already-delivered packets.
func clearBufferedPackets(ctx context.Context, runner db.TxRunner, repositoryID, supervisorID, pipePath string) error {
	if err := runner.Exec(ctx, `
DELETE FROM striatumd.supervisor_buffered_packets
 WHERE repository_id = $1 AND supervisor_id = $2 AND pipe_path = $3`, repositoryID, supervisorID, pipePath); err != nil {
		if isUndefinedTableErr(err) {
			return nil
		}
		return err
	}
	return nil
}
