package mutations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

const escalationNotifyURLEnv = "STRIATUM_ESCALATION_NOTIFY_URL"

var (
	escalationNotifyHTTPClient = &http.Client{Timeout: 3 * time.Second}
	tailnetIPv4Prefix          = netip.MustParsePrefix("100.64.0.0/10")
)

// NotifyEscalationsBestEffort posts one opt-in, best-effort JSON notification
// per newly-created escalation_inbox row. Callers MUST invoke it strictly
// AFTER the transaction that created the rows has committed: it performs
// network I/O (bounded by the 3s client timeout), swallows every error, and
// must never be able to roll back or delay a mutation. It is a no-op unless
// STRIATUM_ESCALATION_NOTIFY_URL names a loopback/tailnet target, and callers
// on dry-run paths must not call it at all. It is exported so producers
// outside this package (the recovery sweep's poison-run breaker trip) can
// share the exact same guardrails instead of growing a second notifier.
func NotifyEscalationsBestEffort(ctx context.Context, repositoryID, runID string, escalations []map[string]any) {
	if len(escalations) == 0 {
		return
	}
	target, ok := escalationNotifyURLFromEnv()
	if !ok {
		return
	}
	for _, escalation := range escalations {
		_ = postRecoveryEscalationNotification(ctx, target, recoveryEscalationNotificationPayload(repositoryID, runID, escalation))
	}
}

func escalationNotifyURLFromEnv() (*url.URL, bool) {
	raw := strings.TrimSpace(os.Getenv(escalationNotifyURLEnv))
	if raw == "" {
		return nil, false
	}
	return parseEscalationNotifyURL(raw)
}

func parseEscalationNotifyURL(raw string) (*url.URL, bool) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, false
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, false
	}
	if target.Host == "" || target.User != nil {
		return nil, false
	}
	if !escalationNotifyHostAllowed(target.Hostname()) {
		return nil, false
	}
	return target, true
}

func escalationNotifyHostAllowed(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return true
		}
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			return false
		}
		addr = addr.Unmap()
		return addr.Is4() && tailnetIPv4Prefix.Contains(addr)
	}
	return strings.HasSuffix(host, ".ts.net")
}

func recoveryEscalationNotificationPayload(repositoryID, runID string, escalation map[string]any) map[string]any {
	kind := notificationString(escalation, "blocker_kind")
	if kind == "" {
		kind = recoveryExhaustedBlockerKind
	}
	// The producer may name itself via a "source" key (e.g. the sweep breaker's
	// recovery.sweep_trip_latch, work.block); the original sweep producer's
	// value stays the default so existing consumers see an unchanged payload.
	source := notificationString(escalation, "source")
	if source == "" {
		source = "recovery.sweep"
	}
	payload := map[string]any{
		"schema_version":  "striatum.escalation_notification.v1",
		"source":          source,
		"repository_id":   repositoryID,
		"run_id":          runID,
		"escalation_kind": kind,
		"blocker_kind":    kind,
	}
	for _, key := range []string{"blocker_id", "job_id", "workflow_job_id", "stall_class", "disposition"} {
		if value := notificationString(escalation, key); value != "" {
			payload[key] = value
		}
	}
	if attempts, ok := escalation["recovery_attempts"]; ok {
		payload["recovery_attempts"] = attempts
	}
	return payload
}

func notificationString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value := nullable(values[key])
	if value == nil {
		return ""
	}
	out := strings.TrimSpace(fmt.Sprint(value))
	if out == "<nil>" {
		return ""
	}
	return out
}

func postRecoveryEscalationNotification(ctx context.Context, target *url.URL, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "striatumd-escalation-notifier")
	resp, err := escalationNotifyHTTPClient.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	closeErr := resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notifier returned HTTP %d", resp.StatusCode)
	}
	return closeErr
}
