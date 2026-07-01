package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// LatestOwnerBundleVersion is the highest owner-DDL bundle the binary ships.
// Owner bundles (RFC 0110 §8.1) carry owner-only DDL — the authority registry,
// SECURITY DEFINER write functions, capability stamps, and the phased DML
// revokes — that the runtime role cannot perform. They are applied OUT-OF-BAND
// as the database owner via `striatum daemon owner-ddl apply`, never through the
// runtime-role ApplyMigrations path (RFC 0079 §5).
//
// RFC 0167 P0 (D260) advanced this past the staged DDL-revoke with the normal
// apply-eligible 0022 bundle. RFC 0168 P0 (D272) advanced it again with the
// normal 0023 lane-uid lease authority reassertion. P1-VERDICT-MODEL-IDENTITY
// advances it with owner-held verdict model stamps. Because normal bundles now
// live above the revoke frontier, the revoke predicates below target the EXACT
// revoke version (== DDLRevokeOwnerBundleVersion) rather than ">= the frontier" —
// otherwise normal bundles above 0021 would be wrongly excluded from apply and a
// watermark MAX above 21 would be misread as "the revoke (21) was applied".
const LatestOwnerBundleVersion = 24

// DDLRevokeOwnerBundleVersion identifies the RFC 0142 P4 C3 DDL-revoke bundle
// (0021, `REVOKE CREATE ON SCHEMA striatumd FROM striatumd_rw`). It is
// DEPLOY-PLAN-TERMINAL ONLY and, under the build's Option B (D7'), STAGED rather
// than embedded: its SQL lives in sql/owner_staged_activation/ (loaded by
// StagedRevokeBundle), NOT in ownerBundleFS — so OwnerBundles() does NOT list it,
// ExpectedFingerprint() does NOT hash it, and RevokeBundleEmbedded() stays FALSE
// for this inert-landing binary (the flag-OFF serve-boot path is byte-identical
// to a pre-P4 binary). It is also EXCLUDED from every `owner-ddl apply` route via
// OwnerDDLApplyBundles() + the in-loop isNonRevokeBundle guard, so its REVOKE can
// ONLY ever be committed as the terminal `striatum daemon deploy` step (M2) by an
// activation binary that embeds it (§4.3). The revoke is gated by the deploy
// cursor + CheckDeployActivation + the STRIATUM_DEPLOY_DECOUPLED flag + its
// terminal placement, NOT the owner-bundle watermark frontier — so a normal
// EMBEDDED owner bundle (RFC 0167 P0's 0022, RFC 0168 P0's 0023, or later) may
// legitimately sit ABOVE this staged ordinal. The frontier therefore advances
// independently while this stays 21.
const DDLRevokeOwnerBundleVersion = 21

// isNonRevokeBundle reports whether an owner bundle version is a normal,
// `owner-ddl apply`-eligible bundle (everything strictly below the DDL-revoke
// frontier). The DDL-revoke bundle (>= DDLRevokeOwnerBundleVersion) is the sole
// exclusion: it must never be applied via the pending loop, the FMA-007 self-heal
// reapply, a nil-fallback, or a test helper — only via the terminal deploy step.
//
// The exclusion targets the EXACT revoke version, not ">= the frontier": RFC 0167
// P0's normal bundle 0022 sits above the staged revoke 0021 and MUST stay
// apply-eligible. Only 0021 itself is the deploy-plan-terminal exclusion.
func isNonRevokeBundle(version int) bool { return version != DDLRevokeOwnerBundleVersion }

// OwnerDDLApplyBundles returns the owner bundles eligible for `owner-ddl apply`:
// the FULL embedded set with any DDL-revoke bundle (>= 0021) filtered OUT (M2). It
// is the single named filter every `owner-ddl apply` route loads, so a revoke can
// never be committed through the owner-ddl path. Under Option B (D7') the revoke
// (0021) is STAGED out of ownerBundleFS, so for THIS inert-landing binary
// OwnerBundles() already lacks it and the filter is a no-op; the in-loop guard +
// this filter remain the load-bearing exclusion for an activation binary that
// embeds 0021 (where they must filter it from every apply route).
func OwnerDDLApplyBundles() ([]OwnerBundle, error) {
	bundles, err := OwnerBundles()
	if err != nil {
		return nil, err
	}
	filtered := make([]OwnerBundle, 0, len(bundles))
	for _, b := range bundles {
		if isNonRevokeBundle(b.Version) {
			filtered = append(filtered, b)
		}
	}
	return filtered, nil
}

// RevokeBundleEmbedded reports whether this binary embeds the DDL-revoke bundle
// (0021) in ownerBundleFS — the `revokeEmbedded` predicate the boot-path deploy
// activation gate (CheckDeployActivation) reads. It is derived from FILE PRESENCE
// in OwnerBundles() at DDLRevokeOwnerBundleVersion, NOT from
// `LatestOwnerBundleVersion >= 21` (which stays 20). Under Option B (D7') this
// build STAGES 0021 outside ownerBundleFS (sql/owner_staged_activation/, loaded by
// StagedRevokeBundle), so for the inert-landing binary OwnerBundles() does not list
// it and this returns FALSE — the shadow-first invariant that keeps the flag-OFF
// serve-boot path byte-identical. An activation binary that embeds 0021 in
// ownerBundleFS returns true and must run on the decoupled path (§3.3a step 0).
func RevokeBundleEmbedded() (bool, error) {
	bundles, err := OwnerBundles()
	if err != nil {
		return false, err
	}
	for _, b := range bundles {
		// EXACT match, not ">=": a normal bundle above the revoke (RFC 0167 P0's
		// 0022) must NOT flip this true. Only the revoke (0021) being embedded does.
		if b.Version == DDLRevokeOwnerBundleVersion {
			return true, nil
		}
	}
	return false, nil
}

// RequiredOwnerBundleVersion is the applied `owner_bundle_meta` watermark the
// serving binary requires before it may apply runtime migrations and serve (RFC
// 0142 Layer 2). It is the owner-bundle frontier the binary was built against —
// the highest bundle it ships — so a binary that ships bundle N expects the
// database to already carry bundle >= N. Owner bundles are applied OUT-OF-BAND
// (`striatum daemon owner-ddl apply`) before the daemon restarts onto new code;
// when that step is skipped, a runtime migration that depends on the bundle's
// ownership/grant topology would die at boot with `42501` and crash-loop
// (RFC 0142 failure #2; #442 / D248). The boot-time watermark interlock turns
// that crash-loop into a clean, actionable halt (apoptosis, not necrosis).
const RequiredOwnerBundleVersion = LatestOwnerBundleVersion

// ErrAwaitingOwnerDDL is the sentinel for the RFC 0142 Layer 2 watermark
// shortfall: the binary requires a higher applied owner-bundle watermark than
// the database carries. It is the typed `awaiting_owner_ddl` halt — the database
// is left UNTOUCHED (no runtime migration is applied) and the daemon refuses to
// serve cleanly rather than force-committing a half-applied forward deploy.
var ErrAwaitingOwnerDDL = errors.New("awaiting_owner_ddl")

// AwaitingOwnerDDLError carries the applied-vs-required owner-bundle versions and
// the exact remediation command for the RFC 0142 Layer 2 watermark shortfall. It
// wraps ErrAwaitingOwnerDDL so callers can branch on the condition with
// errors.Is / errors.As while still printing the operator-facing remediation.
//
// It also carries the sibling "watermark unreadable" case (RFC 0142 two-role
// ownership trap; #581): when the runtime role cannot even SELECT the watermark
// surface (`owner_bundle_meta` is owner-owned and its grants are in flux mid-
// deploy) the read fails with PostgreSQL 42501 (insufficient_privilege). That is
// the SAME clean halt — a runtime-role privilege gap that a pending owner DDL
// closes, which a bare restart cannot fix — so it reuses this type (and so the
// daemon's `exitAwaitingOwnerDDL` non-restartable exit) rather than crash-looping
// as a generic bootstrap error. Cause is set in that branch so Error() renders
// the privilege-specific remediation and Unwrap() exposes the underlying pg error.
type AwaitingOwnerDDLError struct {
	Applied  int
	Required int
	// Cause, when set, marks the "watermark unreadable" variant (42501 reading
	// owner_bundle_meta as the runtime role) instead of the version shortfall.
	// Applied/Required are not meaningful in that case (the read never returned a
	// version), so Error() renders the privilege-specific message.
	Cause error
}

// Error renders the actionable halt message. For the version shortfall it names
// the applied-vs-required versions; for the watermark-unreadable variant (#581)
// it names the privilege gap on owner_bundle_meta. Both end with the same
// out-of-band remediation command and the database-untouched guarantee.
func (e *AwaitingOwnerDDLError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf(
			"awaiting_owner_ddl: the runtime role cannot read the owner-bundle watermark surface "+
				"(`striatumd.owner_bundle_meta`): %v. This is the RFC 0142 two-role ownership trap — "+
				"owner-owned grants are in flux mid-deploy — so apply the pending owner bundle(s) as the "+
				"database owner BEFORE restarting onto this binary: "+
				"`striatum daemon owner-ddl apply` (RFC 0142 Layer 2). The database was left untouched (no runtime migration ran).",
			e.Cause)
	}
	return fmt.Sprintf(
		"awaiting_owner_ddl: applied owner-bundle watermark %d is below the %d this binary requires; "+
			"apply the pending owner bundle(s) as the database owner BEFORE restarting onto this binary: "+
			"`striatum daemon owner-ddl apply` (RFC 0142 Layer 2). The database was left untouched (no runtime migration ran).",
		e.Applied, e.Required)
}

// Unwrap lets errors.Is(err, ErrAwaitingOwnerDDL) match an AwaitingOwnerDDLError
// in both variants, and errors.As reach the underlying pg error for the
// watermark-unreadable variant.
func (e *AwaitingOwnerDDLError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrAwaitingOwnerDDL, e.Cause}
	}
	return []error{ErrAwaitingOwnerDDL}
}

// CheckOwnerBundleWatermark enforces the RFC 0142 Layer 2 owner-bundle watermark
// interlock against the live database, returning the policy decision WITHOUT
// mutating anything. It is called on boot BEFORE ApplyMigrations, so a shortfall
// halts cleanly and leaves the database untouched.
//
//   - applied < required (SHORTFALL): return an *AwaitingOwnerDDLError. The caller
//     must NOT apply runtime migrations — the pending owner DDL is a hard
//     prerequisite, and applying a runtime migration that depends on it would
//     crash-loop the single writer (RFC 0142 failure #2).
//   - applied == required (IN SYNC): nil. Boot proceeds exactly as before.
//   - applied  > required (DOWNGRADE): the running binary is OLDER than the
//     applied watermark. Policy is TOLERATE-FORWARD (the explicit RFC 0142
//     default): a newer schema under an older binary is allowed to serve, so a
//     botched binary rollback does not crash-loop. Return nil. (The symmetric
//     runtime-schema downgrade is already guarded in ApplyMigrations, which
//     refuses a substrate version newer than it supports.)
//
// applied is read via OwnerBundleVersion, which returns 0 when no owner bundle
// (and hence no authority schema) is present — a fresh single-role database. A
// fresh database with no owner bundle is the bootstrap case: required is the
// frontier the binary ships, but there is nothing to apply out-of-band yet
// (single-role deployments never apply owner bundles), so a 0-watermark database
// is treated as the bootstrap/single-role case and NOT halted. Only a database
// that HAS an authority schema (applied >= 1) but lags the required frontier is a
// genuine shortfall.
func CheckOwnerBundleWatermark(ctx context.Context, runner Runner) error {
	applied, err := OwnerBundleVersion(ctx, runner)
	if err != nil {
		// #581 (RFC 0142 two-role ownership trap): reading owner_bundle_meta as the
		// runtime role fails with 42501 (insufficient_privilege) when the owner-owned
		// watermark surface's grants/ownership are in flux mid-deploy. That is the
		// SAME clean, deterministic halt as a version shortfall — a runtime-role
		// privilege gap a pending owner DDL closes, which a bare restart cannot fix —
		// so classify it as awaiting_owner_ddl (the daemon's non-restartable exit)
		// instead of letting it propagate as a generic bootstrap error that
		// crash-loops under systemd Restart=on-failure (apoptosis, not necrosis).
		if isInsufficientPrivilege(err) {
			return &AwaitingOwnerDDLError{Required: RequiredOwnerBundleVersion, Cause: err}
		}
		return err
	}
	// applied == 0 is a fresh / single-role database with no authority schema.
	// Single-role deployments never run owner bundles, so there is nothing to
	// apply out-of-band; the runtime migrations bootstrap the substrate. Halting
	// here would refuse a healthy first boot. Only an authority-bearing database
	// (applied >= 1) that lags the frontier is a true shortfall.
	if applied == 0 {
		return nil
	}
	if applied < RequiredOwnerBundleVersion {
		return &AwaitingOwnerDDLError{Applied: applied, Required: RequiredOwnerBundleVersion}
	}
	// applied >= RequiredOwnerBundleVersion: in-sync or a forward (downgraded
	// binary) schema. TOLERATE-FORWARD: serve.
	return nil
}

//go:embed sql/owner/*.sql
var ownerBundleFS embed.FS

// stagedActivationFS holds the RFC 0142 P4 DDL-revoke bundle (0021) STAGED OUTSIDE
// ownerBundleFS (Option B / D7'). It is a SEPARATE embed.FS, so OwnerBundles() /
// ExpectedFingerprint() / RevokeBundleEmbedded() (which read ownerBundleFS) never
// see it and the flag-OFF serve-boot path stays byte-identical to a pre-P4 binary.
// The deployer still references 0021 from here (StagedRevokeBundle) for the
// activation/verify binary's plan (§4.3 two-binary choreography).
//
//go:embed sql/owner_staged_activation/*.sql
var stagedActivationFS embed.FS

var ownerBundleLabels = map[int]string{
	1:  "authority schema + v3 hash + phase 0 audit_only (RFC 0110 N+1)",
	2:  "runtime read grant on schema_authority for capability parity (RFC 0110 N+1)",
	3:  "phase 1 audit_artifacts: append_artifact_row SD fn + artifacts INSERT revoke (RFC 0110 §7)",
	4:  "phase 2 full: append_event_row SD fn (in-DB v3 hash + transcript exclusion) + events INSERT revoke (RFC 0110 §7)",
	5:  "runtime read scope R1: token-secret projections + clients secret-column SELECT revoke (RFC 0113)",
	6:  "runtime read scope R1 step 2: principal/session identity projections + SELECT revokes + ownership transfer (RFC 0114)",
	7:  "artifact placement column + append_artifact_row placement overload (RFC 0123)",
	8:  "artifact metadata full-text search column and GIN index (RFC 0119 / D179)",
	9:  "build-owned review_generation columns on jobs + verdicts (RFC 0126 P0 / D194 / GH #282)",
	10: "wedged_no_tool_progress liveness stall class on sessions CHECK (GH #324)",
	11: "hot event-read covering index on events (GH #330)",
	12: "quarantined job state on jobs CHECK (per-job quarantine + finalize-the-majority, GH #311)",
	13: "barrier_assembly job_type on jobs CHECK (recoverable fan-in assembly job, RFC 0135 P2 / GH #346)",
	14: "event/audit chain-head lock wait gauges for doctor convoy warnings (GH #372 / GH #379)",
	15: "FK covering indexes on events/audit_log (GH #386)",
	16: "verify job_type on jobs CHECK (executable verification gate / sandboxed verifier lane, RFC 0134 D227 / GH #395)",
	17: "synthetic pipe-read liveness column on sessions for pipe-transport lanes (RFC 0131 131-future / GH #350)",
	18: "transfer pre-split runtime-table cohort (job_recovery_state etc.) ownership to striatumd_rw so runtime migrations may ALTER them (GH #442 / #441)",
	19: "transfer supervisor-pointer table cohort ownership to striatumd_rw so runtime migration 0039 can reshape idx_process_supervisor_pointers_run (RFC 0139 / GH #421)",
	20: "runtime read grant on owner_bundle_meta for owner-bundle watermark boot interlock (RFC 0142 Layer 2 / GH #581)",
	21: "serving-role create-DDL revocation: REVOKE CREATE ON SCHEMA striatumd FROM striatumd_rw (RFC 0142 P4 C3, deploy-plan-terminal / D262)",
	22: "operator identity & run attribution: operator_handles + operator_sessions + runs write-once origin stamp + DEFINER identity projections + composed-route read closure (RFC 0167 P0 / D260)",
	23: "lane uid lease authority reassertion for RFC 0168 P0 / D272",
	24: "declared model identity stamps on verdicts (P1-VERDICT-MODEL-IDENTITY)",
}

// OwnerBundle is one versioned owner-DDL bundle file.
type OwnerBundle struct {
	Version int
	Label   string
	Path    string
	SQL     string
}

// SHA256 is the content hash recorded in owner_bundle_meta on apply.
func (b OwnerBundle) SHA256() string {
	sum := sha256.Sum256([]byte(b.SQL))
	return hex.EncodeToString(sum[:])
}

// OwnerBundles returns the embedded owner bundles in ascending version order.
func OwnerBundles() ([]OwnerBundle, error) {
	entries, err := ownerBundleFS.ReadDir("sql/owner")
	if err != nil {
		return nil, err
	}
	var bundles []OwnerBundle
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("owner bundle %s has no leading version: %w", entry.Name(), err)
		}
		body, err := ownerBundleFS.ReadFile("sql/owner/" + entry.Name())
		if err != nil {
			return nil, err
		}
		bundles = append(bundles, OwnerBundle{
			Version: version,
			Label:   ownerBundleLabels[version],
			Path:    "sql/owner/" + entry.Name(),
			SQL:     string(body),
		})
	}
	sort.Slice(bundles, func(i, j int) bool { return bundles[i].Version < bundles[j].Version })
	return bundles, nil
}

// StagedRevokeBundle loads the DDL-revoke bundle (0021) from stagedActivationFS —
// the staged-but-not-embedded home it lives in under Option B (D7'). It returns
// (bundle, true, nil) when a staged revoke file is present (this binary keeps the
// activation SQL version-controlled and loadable for the activation/verify run's
// deploy plan), and (zero, false, nil) when the staged dir holds no revoke file.
// It is the seam the SEED names ("the deployer still references 0021 by loading it
// from the staged dir"): the activation/verify binary's BuildPlan / VerifyStoredTranscript
// resolve the terminal revoke through here, while serve-boot (which reads only
// ownerBundleFS via OwnerBundles/RevokeBundleEmbedded) never sees it.
func StagedRevokeBundle() (OwnerBundle, bool, error) {
	entries, err := stagedActivationFS.ReadDir("sql/owner_staged_activation")
	if err != nil {
		return OwnerBundle{}, false, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if err != nil {
			return OwnerBundle{}, false, fmt.Errorf("staged activation bundle %s has no leading version: %w", entry.Name(), err)
		}
		if version < DDLRevokeOwnerBundleVersion {
			// Only the revoke frontier (>= 21) is staged here; ignore anything else.
			continue
		}
		body, err := stagedActivationFS.ReadFile("sql/owner_staged_activation/" + entry.Name())
		if err != nil {
			return OwnerBundle{}, false, err
		}
		return OwnerBundle{
			Version: version,
			Label:   ownerBundleLabels[version],
			Path:    "sql/owner_staged_activation/" + entry.Name(),
			SQL:     string(body),
		}, true, nil
	}
	return OwnerBundle{}, false, nil
}

// OwnerBundleVersion returns the highest owner bundle version applied to the
// database, or 0 when no bundle (and hence no owner_bundle_meta table) exists.
func OwnerBundleVersion(ctx context.Context, runner Runner) (int, error) {
	present, err := runner.QueryScalar(ctx,
		"SELECT (to_regclass('striatumd.owner_bundle_meta') IS NOT NULL)::text")
	if err != nil {
		return 0, err
	}
	if present != "true" {
		return 0, nil
	}
	value, err := runner.QueryScalar(ctx,
		"SELECT COALESCE(MAX(version), 0)::text FROM striatumd.owner_bundle_meta")
	if err != nil {
		return 0, err
	}
	version, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	return version, nil
}

// IsOwnerBundleApplied reports whether a SPECIFIC owner-bundle version is recorded
// as applied in owner_bundle_meta — decoupled from the MAX watermark. It exists
// because, since RFC 0167 P0's normal bundle 0022 sits above the staged DDL-revoke
// (0021), MAX(version) >= 21 no longer implies "the revoke was applied" (it is 22
// with 21 absent). The forward-watermark serve barrier (connection.go) must ask
// "was 0021 specifically applied?", which this answers. Returns false (no error)
// on a fresh / single-role database with no owner_bundle_meta table.
func IsOwnerBundleApplied(ctx context.Context, runner Runner, version int) (bool, error) {
	present, err := runner.QueryScalar(ctx,
		"SELECT (to_regclass('striatumd.owner_bundle_meta') IS NOT NULL)::text")
	if err != nil {
		return false, err
	}
	if present != "true" {
		return false, nil
	}
	value, err := runner.QueryScalar(ctx,
		"SELECT EXISTS(SELECT 1 FROM striatumd.owner_bundle_meta WHERE version = $1)::text", version)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(value) == "true", nil
}

// ApplyOwnerBundles applies every owner bundle newer than the recorded version,
// as the owner role behind runner. Each version applies in a single transaction
// that stamps owner_bundle_meta last, so a partially-applied bundle cannot
// persist (atomic per version); re-applying a stamped version is a no-op. It
// returns the versions applied this call and the resulting version.
//
// FMA-007 / GH #458 self-heal: the normal apply path skips any bundle whose
// version is already stamped, so if an *earlier* bundle's objects went missing
// (a credential/ownership skew or a hand-edited database) the later bundle that
// depends on them fails on a missing cross-bundle object. When that happens this
// re-applies *every* shipped bundle in ascending order once — each bundle is
// idempotent DDL in its own transaction, so a missing earlier object is
// re-created before the later bundle that needs it — and retries. Only if the
// ordered reconciliation still cannot satisfy the dependency does it return a
// legible, actionable error; the daemon stays fail-closed as the final safety
// property. The reconciliation never creates objects outside the bundle DDL.
func ApplyOwnerBundles(ctx context.Context, runner Runner, daemonVersion string) ([]int, int, error) {
	if daemonVersion == "" {
		daemonVersion = "dev"
	}
	// M2: load the NON-REVOKE apply slice (0021 filtered out). The DDL-revoke
	// bundle is deploy-plan-terminal and must never be committed through this
	// route — only via `striatum daemon deploy`.
	bundles, err := OwnerDDLApplyBundles()
	if err != nil {
		return nil, 0, err
	}
	current, err := OwnerBundleVersion(ctx, runner)
	if err != nil {
		return nil, 0, err
	}
	applied, current, err := applyPendingOwnerBundles(ctx, runner, bundles, current, daemonVersion)
	if err == nil {
		return applied, current, nil
	}
	// A non-dependency failure (or a reconciliation already attempted) is
	// returned as-is, fail-closed.
	if !isCrossBundleDependencyError(err) {
		return applied, current, err
	}
	// Ordered idempotent reconciliation: re-apply every shipped bundle so a
	// missing earlier object is re-created before the later bundle depends on
	// it. Each bundle is idempotent, so already-present objects are no-ops.
	reapplied, reErr := ReapplyAllOwnerBundles(ctx, runner, bundles, daemonVersion)
	if reErr != nil {
		// Reconciliation could not heal the gap (e.g. the missing object is not
		// produced by any earlier bundle, or the owner lacks the privilege to
		// re-create it). Surface the actionable message; fail-closed.
		return applied, current, reErr
	}
	// Reconciliation re-ran the full bundle set; recompute the resulting state
	// and report every version it touched.
	final, verErr := OwnerBundleVersion(ctx, runner)
	if verErr != nil {
		return reapplied, current, verErr
	}
	return reapplied, final, nil
}

// applyPendingOwnerBundles applies, in ascending order, each bundle newer than
// current. A failure on a missing cross-bundle object is wrapped legibly so the
// caller (and the operator) sees which bundle failed, which object is missing,
// and the one-step remediation, instead of a raw `relation does not exist`.
func applyPendingOwnerBundles(ctx context.Context, runner Runner, bundles []OwnerBundle, current int, daemonVersion string) ([]int, int, error) {
	var applied []int
	for _, bundle := range bundles {
		if bundle.Version <= current {
			continue
		}
		// M2 in-loop guard: even if an unfiltered list reaches here, the
		// DDL-revoke bundle is never applied via the pending loop.
		if !isNonRevokeBundle(bundle.Version) {
			continue
		}
		if err := applyOneOwnerBundle(ctx, runner, bundle, daemonVersion); err != nil {
			return applied, current, wrapOwnerBundleApplyError(bundle, err)
		}
		applied = append(applied, bundle.Version)
		current = bundle.Version
	}
	return applied, current, nil
}

// ReapplyAllOwnerBundles re-applies every shipped owner bundle in ascending
// numeric order, regardless of the recorded version. Each bundle is idempotent
// DDL applied in its own transaction (so an already-present object is a no-op
// and the stamp INSERT is ON CONFLICT DO NOTHING), which makes this the
// ordered self-heal for a database whose earlier-bundle objects went missing
// (FMA-007 / GH #458): a missing object is re-created by its owning bundle
// before any later bundle depends on it. It returns the versions it (re)ran.
// A failure is wrapped with the same legible remediation as the normal path.
func ReapplyAllOwnerBundles(ctx context.Context, runner Runner, bundles []OwnerBundle, daemonVersion string) ([]int, error) {
	if daemonVersion == "" {
		daemonVersion = "dev"
	}
	if bundles == nil {
		// M2 nil-fallback split: the self-heal reconciler loads the NON-REVOKE
		// apply slice, so a forced FMA-007 reapply re-creates earlier bundles'
		// objects WITHOUT ever committing the DDL-revoke early.
		loaded, err := OwnerDDLApplyBundles()
		if err != nil {
			return nil, err
		}
		bundles = loaded
	}
	var ran []int
	for _, bundle := range bundles {
		// M2 in-loop guard: never reapply the DDL-revoke bundle, even if a caller
		// passed the full unfiltered list.
		if !isNonRevokeBundle(bundle.Version) {
			continue
		}
		if err := applyOneOwnerBundle(ctx, runner, bundle, daemonVersion); err != nil {
			return ran, wrapOwnerBundleApplyError(bundle, err)
		}
		ran = append(ran, bundle.Version)
	}
	return ran, nil
}

// crossBundleDependencySQLStates are the PostgreSQL undefined-object codes a
// bundle raises when an object an earlier bundle should have created is absent:
// undefined_table (relation does not exist), undefined_column, undefined_function,
// and the generic undefined_object. These are the FMA-007 cross-bundle failures.
var crossBundleDependencySQLStates = map[string]struct{}{
	"42P01": {}, // undefined_table
	"42703": {}, // undefined_column
	"42883": {}, // undefined_function
	"42704": {}, // undefined_object
}

// isCrossBundleDependencyError reports whether err is a missing-object failure
// of the kind a re-applied later bundle raises when an earlier bundle's object
// is gone (the FMA-007 self-heal trigger).
func isCrossBundleDependencyError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	_, ok := crossBundleDependencySQLStates[pgErr.Code]
	return ok
}

// wrapOwnerBundleApplyError annotates a bundle-apply failure. For a missing
// cross-bundle object it produces an actionable message naming the failing
// bundle, the missing object, and the one-step remediation; other errors keep
// the existing wrapping so unrelated failures stay legible too.
func wrapOwnerBundleApplyError(bundle OwnerBundle, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if _, ok := crossBundleDependencySQLStates[pgErr.Code]; ok {
			missing := strings.TrimSpace(pgErr.Message)
			if missing == "" {
				missing = "an object an earlier owner bundle should have created"
			}
			return fmt.Errorf(
				"apply owner bundle %d (%s): missing cross-bundle dependency (%s: %s); "+
					"re-run `striatum daemon owner-ddl apply` to re-create the earlier bundle's objects in order, "+
					"and if it still fails restore the missing object as the database owner: %w",
				bundle.Version, bundle.Label, pgErr.Code, missing, err)
		}
	}
	return fmt.Errorf("apply owner bundle %d (%s): %w", bundle.Version, bundle.Label, err)
}

// capabilityProtectedTable maps each SD-append capability stamp to the table
// whose direct runtime-role INSERT that phase revokes (RFC 0110 §7). The
// protected set is derived from the stamps rather than a static list so a
// deployment revokes exactly the surfaces its applied owner bundles have closed
// — never a surface whose bundle is unapplied (which would break that surface's
// writes for a daemon that has not adopted the phase).
var capabilityProtectedTable = map[string]string{
	"audit_sd_append":    "audit_log",
	"artifact_sd_append": "artifacts",
	"event_sd_append":    "events",
}

// ReassertWriteRevokes re-applies the protected-surface INSERT revokes for every
// phase whose capability the owner bundle has stamped (RFC 0110 §6,
// C-GRANT-DRIFT). Any privilege-granting step (migration helper, doctor
// repair-grants) must call this afterwards so a stray GRANT cannot quietly
// reopen a closed surface. Run as the owner; a no-op when no authority schema is
// present.
func ReassertWriteRevokes(ctx context.Context, runner Runner) error {
	stamped, present, err := readStampedCapabilities(ctx, runner)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	for _, capability := range stamped {
		table, ok := capabilityProtectedTable[capability]
		if !ok {
			continue
		}
		if err := runner.Exec(ctx, fmt.Sprintf(
			"REVOKE INSERT ON striatumd.%s FROM striatumd_rw", table)); err != nil {
			return fmt.Errorf("reassert revoke on %s: %w", table, err)
		}
	}
	return nil
}

// readScopeReasserts maps each read-projection capability stamp to the revoke
// + grant-back statements its owner bundle uses to close the surface (the read
// analog of capabilityProtectedTable). Stamps drive re-assertion, so a
// deployment re-closes exactly the surfaces its applied bundles closed — never
// a surface whose bundle is unapplied. The statement lists restate the bundle
// SQL verbatim; the bundle and this map together fully determine the
// post-close ACL.
var readScopeReasserts = map[string][]string{
	// Bundle 0005 (RFC 0113 R1): only the token secret columns on clients are
	// closed; non-secret client metadata stays directly readable.
	"auth_projection_read": {
		"REVOKE SELECT ON striatumd.clients FROM striatumd_rw",
		`GRANT SELECT (
		  client_id, client_kind, display_name, token_id,
		  created_at, expires_at, revoked_at, last_used_at
		) ON striatumd.clients TO striatumd_rw`,
	},
	// Bundle 0006 (RFC 0114): principals and client_sessions are fully read
	// denied; principal_clients keeps the column gate (principal_id denied,
	// client_id/linked_at/unlinked_at preserved for the live UPDATE ... WHERE
	// in admin/tokens.go). DML grant-backs are restated so a drift repair
	// cannot strand the write surface.
	"identity_projection_read": {
		"REVOKE SELECT ON striatumd.client_sessions FROM striatumd_rw",
		"GRANT INSERT, UPDATE, DELETE ON striatumd.client_sessions TO striatumd_rw",
		"REVOKE SELECT ON striatumd.principals FROM striatumd_rw",
		"GRANT INSERT, UPDATE, DELETE ON striatumd.principals TO striatumd_rw",
		"REVOKE SELECT ON striatumd.principal_clients FROM striatumd_rw",
		`GRANT SELECT (client_id, linked_at, unlinked_at)
		  ON striatumd.principal_clients TO striatumd_rw`,
		"GRANT INSERT, UPDATE, DELETE ON striatumd.principal_clients TO striatumd_rw",
	},
	// Bundle 0022 (RFC 0167 P0): the composed-route read closure. Restated as
	// REVOKE-then-column-GRANT so the reassert converges to the same closed ACL
	// whether or not a prior stray table-wide GRANT exists (drift-proof). runs is
	// owner-held, so its REVOKE is irreversible by striatumd_rw; the operator_*
	// tables are new + column-granted from creation but are restated here so a
	// drift GRANT cannot widen them. The statement lists restate the bundle SQL
	// verbatim — keep them in lockstep with 0022.
	"operator_identity_run_attribution": {
		// Route 2: runs column gate (created_by_principal_id excluded). The list is
		// the live catalog MINUS created_by_principal_id: the 0005 baseline + 0025's
		// completion_mode + 0026's completion_record_json + 0022's created_by_handle_id.
		// Keep in lockstep with the 0022 bundle SQL (omitting 0025/0026 strands a
		// 42501 on the run.summary / evidence.export runtime readers).
		"REVOKE SELECT ON striatumd.runs FROM striatumd_rw",
		`GRANT SELECT (
		  repository_id, run_id, workflow_snapshot_id, repo_root, state,
		  branch_name, branch_base, branch_confirmed_at, branch_confirmed_by, created_at,
		  started_at, completed_at, stop_reason, paused_at, paused_reason, cross_repo_run_id,
		  completion_mode, completion_record_json,
		  created_by_handle_id
		) ON striatumd.runs TO striatumd_rw`,
		// Route 1: operator_handles column gate (principal_id excluded).
		"REVOKE SELECT ON striatumd.operator_handles FROM striatumd_rw",
		`GRANT SELECT (handle_id, leased_session_id, handle, repository_id,
		  released_at, leased_until, last_heartbeat_at)
		  ON striatumd.operator_handles TO striatumd_rw`,
		// operator_sessions column gate (C2'): principal_id AND client_id excluded.
		"REVOKE SELECT ON striatumd.operator_sessions FROM striatumd_rw",
		`GRANT SELECT (operator_session_id, repository_id, state, registered_at,
		  last_heartbeat_at, expires_at, closed_at, close_reason)
		  ON striatumd.operator_sessions TO striatumd_rw`,
	},
}

var runtimeParityReadReasserts = []string{
	"GRANT SELECT ON striatumd.schema_authority TO striatumd_rw",
	"GRANT SELECT ON striatumd.owner_bundle_meta TO striatumd_rw",
}

// ReassertReadRevokes re-applies read-scope revokes for authority-protected
// read projections stamped by owner bundles and restates the startup-parity
// read grants that are intentionally runtime-readable. Like
// ReassertWriteRevokes it is map-driven from the stamps for closed projection
// surfaces, so it re-closes exactly the read surfaces the applied bundles
// closed. Run as the owner; a no-op when no authority schema is present.
// `striatum daemon owner-ddl apply` calls it after ApplyOwnerBundles, making a
// re-run the documented grant-drift repair.
func ReassertReadRevokes(ctx context.Context, runner Runner) error {
	stamped, present, err := readStampedCapabilities(ctx, runner)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	for _, stmt := range runtimeParityReadReasserts {
		if err := runner.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("reassert startup parity read grant: %w", err)
		}
	}
	for _, capability := range stamped {
		stmts, ok := readScopeReasserts[capability]
		if !ok {
			continue
		}
		for _, stmt := range stmts {
			if err := runner.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("reassert read revokes for %s: %w", capability, err)
			}
		}
	}
	return nil
}

func applyOneOwnerBundle(ctx context.Context, runner Runner, bundle OwnerBundle, daemonVersion string) error {
	tx, err := runner.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()
	if err := tx.Exec(ctx, bundle.SQL); err != nil {
		return err
	}
	// The bundle creates owner_bundle_meta (IF NOT EXISTS); the stamp is the
	// last write in the same transaction, so the marker and the objects commit
	// together or not at all.
	if err := tx.Exec(ctx,
		`INSERT INTO striatumd.owner_bundle_meta(version, label, sha256, daemon_version)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (version) DO NOTHING`,
		bundle.Version, bundle.Label, bundle.SHA256(), daemonVersion,
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}
