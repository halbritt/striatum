package mutations

import (
	"context"
	"errors"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

// HandleVerifierAttest is the RFC 0141 / D243 (#482) operator-token attestation
// minter: the authoritative, daemon-owned, fail-closed PINNED → VERIFIABLE hinge.
//
// RFC 0141 (D238/D239) shipped the verification_gate at EXPERIMENTAL tier with a
// documented forge-able gap: attestation was enforced ONLY at the `striatum verifier
// attest` VERB level (it refused a lane/session context and wrote a repo-file
// `allowlist.pins.<fp>.attest.json` sidecar), and the run-completion gate never
// re-verified it. A non-compliant lane could forge that sidecar, join → VERIFIABLE,
// run the real pinned external check, author the claim VERIFIED, and the daemon gate
// would honor it — attestation was lane-side honesty, not a gate-enforced trust
// boundary.
//
// This handler moves the authoritative attestation into daemon-owned PG (the product
// boundary), where the run-completion gate reads it (attestationCovers). It is:
//
//   - CapabilityAdmin (operator) — wired in registry_methods.go / the contract.
//   - REFUSED for any session-bound capability token (auth.IsSessionBound()): the
//     trust signal must come from a principal the verified lane cannot impersonate.
//     This is the DAEMON-ENFORCED equivalent of the verb's STRIATUM_SESSION_ID
//     refusal, now backed by the DB session_id capability flag (RFC 0096 V2 / #135),
//     not an env var a lane could unset.
//
// It binds the attestation to the EXACT pinned bytes (binary_sha256), so a later
// re-pin of different bytes does NOT inherit the blessing: the gate read of a receipt
// sealed to the new bytes finds no matching row and decays to ASSERTED.
func HandleVerifierAttest(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	checkID := strings.TrimSpace(stringParam(envelope, "check_id"))
	binarySHA256 := strings.ToLower(strings.TrimSpace(stringParam(envelope, "binary_sha256")))
	if checkID == "" || binarySHA256 == "" {
		return nil, rpc.NewError("schema_invalid", "verifier.attest requires check_id and binary_sha256 (an attestation blesses an exact check at exact bytes)", nil)
	}

	// The daemon-enforced operator-only gate: a session-bound token can never bless
	// pins. Capability=admin already excludes lane tokens at the dispatch layer, but
	// re-assert the session-binding refusal here so the trust boundary is explicit and
	// holds even for a hypothetical admin-capable session token.
	if auth, ok := rpc.AuthFromContext(ctx); ok && auth.IsSessionBound() {
		return nil, rpc.NewError(
			"capability_denied",
			"verifier.attest refused: the capability token is bound to a session/lane; attestation requires an OPERATOR (session-unbound) token — the verified lane can never bless its own pins",
			map[string]any{"session_id": auth.SessionID},
		)
	}

	attestedBy := ""
	if auth, ok := rpc.AuthFromContext(ctx); ok {
		attestedBy = auth.ClientID
	}

	attestationID, err := newID("att")
	if err != nil {
		return nil, err
	}

	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// Idempotent upsert keyed by the active (repo, check, exact bytes) unique
		// index: re-attesting the same bytes refreshes attested_at/attested_by and
		// returns the existing row rather than minting a duplicate. A previously
		// revoked row for these exact bytes is re-blessed (revoked_at cleared).
		var existingID string
		err := tx.QueryRow(ctx, `
			SELECT attestation_id
			  FROM striatumd.verifier_attestations
			 WHERE repository_id = $1 AND check_id = $2 AND lower(binary_sha256) = $3
			   AND revoked_at IS NULL
			 LIMIT 1`, repositoryID, checkID, binarySHA256).Scan(&existingID)
		switch {
		case err == nil:
			// Already actively attested at these exact bytes — refresh attribution.
			if err := tx.Exec(ctx, `
				UPDATE striatumd.verifier_attestations
				   SET attested_at = now(), attested_by = $1
				 WHERE repository_id = $2 AND attestation_id = $3`,
				attestedBy, repositoryID, existingID); err != nil {
				return nil, err
			}
			attestationID = existingID
		case errors.Is(err, pgx.ErrNoRows):
			if err := tx.Exec(ctx, `
				INSERT INTO striatumd.verifier_attestations (
				  repository_id, attestation_id, check_id, binary_sha256, attested_by
				) VALUES ($1, $2, $3, $4, $5)`,
				repositoryID, attestationID, checkID, binarySHA256, nullable(attestedBy)); err != nil {
				return nil, err
			}
		default:
			return nil, err
		}

		return map[string]any{
			"status":         "attested",
			"attestation_id": attestationID,
			"check_id":       checkID,
			"binary_sha256":  binarySHA256,
			"attested_by":    attestedBy,
		}, nil
	})
}
