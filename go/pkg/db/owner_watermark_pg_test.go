package db_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// TestCheckOwnerBundleWatermarkAgainstLiveDB exercises the RFC 0142 Layer 2
// watermark interlock against a REAL `owner_bundle_meta` table (the policy
// reading the live applied version), covering the three boot decisions:
//
//   - fresh database (no owner bundle, applied == 0): bootstrap, no halt;
//   - fully applied (applied == frontier): in-sync, no halt;
//   - stamped below the frontier (applied < required): typed awaiting_owner_ddl halt.
//
// It also confirms ConnectAndMigrate surfaces the typed shortfall on boot.
func TestCheckOwnerBundleWatermarkAgainstLiveDB(t *testing.T) {
	ctx := context.Background()

	t.Run("fresh_db_bootstrap_no_halt", func(t *testing.T) {
		pool := pgtest.Pool(t)
		// pgtest migrates the substrate but applies no owner bundle, so the watermark
		// is 0 — the bootstrap/single-role case, which must NOT halt.
		if v, err := db.OwnerBundleVersion(ctx, pool.Runner); err != nil || v != 0 {
			t.Fatalf("fresh owner bundle version = %d, err = %v; want 0", v, err)
		}
		if err := db.CheckOwnerBundleWatermark(ctx, pool.Runner); err != nil {
			t.Fatalf("fresh database must not halt, got: %v", err)
		}
	})

	t.Run("fully_applied_in_sync_no_halt", func(t *testing.T) {
		pool := pgtest.Pool(t)
		if _, version, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
			t.Fatalf("apply owner bundles: %v", err)
		} else if version != db.RequiredOwnerBundleVersion {
			t.Fatalf("applied version = %d; want frontier %d", version, db.RequiredOwnerBundleVersion)
		}
		if err := db.CheckOwnerBundleWatermark(ctx, pool.Runner); err != nil {
			t.Fatalf("in-sync database must not halt, got: %v", err)
		}
	})

	t.Run("stamped_below_frontier_halts_typed", func(t *testing.T) {
		pool := pgtest.Pool(t)
		// Apply the real bundles, then forge a lagging watermark by deleting every
		// stamp above 1 so the recorded applied version is 1 (< frontier). The
		// authority objects stay present; only the recorded watermark lags — exactly
		// the deploy-ordering shortfall RFC 0142 Layer 2 catches.
		if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
			t.Fatalf("apply owner bundles: %v", err)
		}
		if err := pool.Runner.Exec(ctx,
			"DELETE FROM striatumd.owner_bundle_meta WHERE version > 1"); err != nil {
			t.Fatalf("forge lagging watermark: %v", err)
		}
		if v, err := db.OwnerBundleVersion(ctx, pool.Runner); err != nil || v != 1 {
			t.Fatalf("forged watermark = %d, err = %v; want 1", v, err)
		}

		err := db.CheckOwnerBundleWatermark(ctx, pool.Runner)
		if err == nil {
			t.Fatal("a watermark below the frontier must halt with awaiting_owner_ddl")
		}
		if !errors.Is(err, db.ErrAwaitingOwnerDDL) {
			t.Fatalf("shortfall error must be ErrAwaitingOwnerDDL, got: %v", err)
		}
		var typed *db.AwaitingOwnerDDLError
		if !errors.As(err, &typed) || typed.Applied != 1 || typed.Required != db.RequiredOwnerBundleVersion {
			t.Fatalf("typed shortfall applied/required = %v; want 1/%d", err, db.RequiredOwnerBundleVersion)
		}

		// ConnectAndMigrate on the same database must surface the same typed halt
		// (the boot path refuses cleanly rather than crash-looping).
		boot, _, bootErr := db.ConnectAndMigrate(ctx, pool.URL, "watermark-pgtest")
		if boot != nil {
			boot.Close()
		}
		if !errors.Is(bootErr, db.ErrAwaitingOwnerDDL) {
			t.Fatalf("ConnectAndMigrate must surface the typed shortfall on boot, got: %v", bootErr)
		}
	})
}

// TestConnectAndMigrateLeavesDBUntouchedOnShortfall proves the RFC 0142 Layer 2
// fail-clean property end to end against a real database: when the recorded
// owner-bundle watermark lags the frontier, ConnectAndMigrate halts BEFORE
// applying any runtime migration, so the substrate version recorded in the DB is
// not advanced by the halted boot — the database is left untouched.
func TestConnectAndMigrateLeavesDBUntouchedOnShortfall(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)

	// Record the substrate version the healthy migrate reached, then forge a
	// lagging owner-bundle watermark and roll the recorded substrate version BACK
	// by one so a (forbidden) runtime migration pass would be observable as an
	// advance. The watermark interlock must prevent that pass entirely.
	before, err := db.ReadSchemaVersion(ctx, pool.Runner)
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundles: %v", err)
	}
	if err := pool.Runner.Exec(ctx,
		"DELETE FROM striatumd.owner_bundle_meta WHERE version > 1"); err != nil {
		t.Fatalf("forge lagging watermark: %v", err)
	}
	rolledBack := before - 1
	if err := pool.Runner.Exec(ctx,
		"UPDATE striatumd.schema_meta SET value = $1 WHERE key = 'substrate_version'",
		strconv.Itoa(rolledBack)); err != nil {
		t.Fatalf("roll back recorded substrate version: %v", err)
	}

	boot, _, bootErr := db.ConnectAndMigrate(ctx, pool.URL, "watermark-pgtest")
	if boot != nil {
		boot.Close()
	}
	if !errors.Is(bootErr, db.ErrAwaitingOwnerDDL) {
		t.Fatalf("ConnectAndMigrate must halt on the shortfall, got: %v", bootErr)
	}

	// The halted boot must not have run ApplyMigrations: the recorded substrate
	// version is still the rolled-back value (untouched), not re-advanced.
	after, err := db.ReadSchemaVersion(ctx, pool.Runner)
	if err != nil {
		t.Fatalf("re-read schema version: %v", err)
	}
	if after != rolledBack {
		t.Fatalf("recorded substrate version = %d after a halted boot; want %d (DB must be left untouched — no migration ran)", after, rolledBack)
	}
}
