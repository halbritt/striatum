package mutations

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

func attestEnvelope(checkID, sha string) rpc.Envelope {
	return rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_attest",
		Method:        "verifier.attest",
		Params: map[string]any{
			"repository_id": "repo_claim",
			"check_id":      checkID,
			"binary_sha256": sha,
		},
	}
}

// TestVerifierAttestOperatorMintsRow verifies the operator-token RPC writes the
// authoritative daemon-owned attestation row (RFC 0141 / D243 / #482), and that the
// row is exactly what the run-completion gate consults — minted here, an external
// two-signal receipt would now read VERIFIED.
func TestVerifierAttestOperatorMintsRow(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	_ = setupClaimLedgerRun(t, ctx, runner)

	// An operator (session-UNBOUND) token: no SessionID.
	opCtx := rpc.WithAuthContext(ctx, rpc.AuthContext{Decision: "allowed", ClientID: "op-1", RepositoryID: "repo_claim"})
	res, err := HandleVerifierAttest(opCtx, runner, attestEnvelope("go-test", "ABC123"))
	if err != nil {
		t.Fatalf("operator attest must succeed: %v", err)
	}
	if res["status"] != "attested" {
		t.Fatalf("result = %#v, want status attested", res)
	}
	// The gate read sees it (sha is normalized to lowercase on both write and read).
	if !attestationCovers(ctx, runner, "repo_claim", "go-test", "abc123") {
		t.Fatal("the gate must see the operator-minted attestation row")
	}
	if attestationCovers(ctx, runner, "repo_claim", "go-test", "deadbeef") {
		t.Fatal("the gate must NOT see an attestation for different bytes")
	}

	// Idempotent: re-attesting the same bytes returns the same active row.
	res2, err := HandleVerifierAttest(opCtx, runner, attestEnvelope("go-test", "abc123"))
	if err != nil {
		t.Fatalf("re-attest must succeed: %v", err)
	}
	if fmt.Sprint(res2["attestation_id"]) != fmt.Sprint(res["attestation_id"]) {
		t.Fatalf("re-attest of the same bytes must reuse the active row, got %v then %v", res["attestation_id"], res2["attestation_id"])
	}
}

// TestVerifierAttestRefusesSessionBoundToken is the daemon-enforced trust boundary:
// a session/lane-bound capability token can never mint an attestation, even if it
// somehow carried the admin capability — the verified lane must never bless its own
// pins. This is the gate-side equivalent of the verb's STRIATUM_SESSION_ID refusal,
// now backed by the DB session_id flag rather than an env var.
func TestVerifierAttestRefusesSessionBoundToken(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	_ = setupClaimLedgerRun(t, ctx, runner)

	laneCtx := rpc.WithAuthContext(ctx, rpc.AuthContext{
		Decision: "allowed", ClientID: "lane-1", RepositoryID: "repo_claim", SessionID: "sess_build",
	})
	_, err := HandleVerifierAttest(laneCtx, runner, attestEnvelope("go-test", "abc123"))
	if err == nil {
		t.Fatal("a session-bound token must be refused")
	}
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != "capability_denied" {
		t.Fatalf("want capability_denied, got %v", err)
	}
	// And no row was written — the forge buys nothing.
	if attestationCovers(ctx, runner, "repo_claim", "go-test", "abc123") {
		t.Fatal("a refused attest must not write a row")
	}
}

// TestVerifierAttestRequiresCheckAndSha — the handler refuses an attestation that
// does not bind an exact check at exact bytes.
func TestVerifierAttestRequiresCheckAndSha(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	_ = setupClaimLedgerRun(t, ctx, runner)
	opCtx := rpc.WithAuthContext(ctx, rpc.AuthContext{Decision: "allowed", ClientID: "op-1", RepositoryID: "repo_claim"})

	if _, err := HandleVerifierAttest(opCtx, runner, attestEnvelope("", "abc")); err == nil {
		t.Fatal("missing check_id must be refused")
	}
	if _, err := HandleVerifierAttest(opCtx, runner, attestEnvelope("go-test", "")); err == nil {
		t.Fatal("missing binary_sha256 must be refused")
	}
}
