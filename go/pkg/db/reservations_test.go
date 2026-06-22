package db

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestReservationLedgerMatchesOnDisk is the RFC 0142 Layer 0 (D258)
// collision guard. It parses the on-disk migration / owner-bundle ordinals in
// both sql dirs AND the committed RESERVATIONS.toml ledger, and FAILS if they
// disagree — a file with no ledger row, a ledger row with no file, a DUPLICATE
// ordinal, or a GAP in the sequence. Because this is a normal go test in pkg/db,
// CI's `go test` IS the collision guard: a new migration must be registered in
// RESERVATIONS.toml (and its file must exist) or this test reds, so two PRs can
// no longer silently grab the same NNNN (the 0039 / 0041 collisions, RFC 0142
// failure #4).
func TestReservationLedgerMatchesOnDisk(t *testing.T) {
	ledger, err := Reservations()
	if err != nil {
		t.Fatalf("parse RESERVATIONS.toml: %v", err)
	}

	t.Run("runtime_migrations", func(t *testing.T) {
		onDisk := ordinalsToFilesOnDisk(t, "sql")
		assertLedgerMatchesDir(t, "runtime_migration", ledger.RuntimeMigrations, onDisk)
		// Frontier sanity: the highest reserved runtime ordinal is the version the
		// loader ships (LatestDaemonDBVersion), so the ledger cannot silently lag
		// the embedded migration set.
		if got := highestOrdinal(ledger.RuntimeMigrations); got != LatestDaemonDBVersion {
			t.Fatalf("runtime-migration frontier in RESERVATIONS.toml = %d, want LatestDaemonDBVersion = %d", got, LatestDaemonDBVersion)
		}
	})

	t.Run("owner_bundles", func(t *testing.T) {
		onDisk := ordinalsToFilesOnDisk(t, "sql/owner")
		assertLedgerMatchesDir(t, "owner_bundle", ledger.OwnerBundles, onDisk)
		if got := highestOrdinal(ledger.OwnerBundles); got != LatestOwnerBundleVersion {
			t.Fatalf("owner-bundle frontier in RESERVATIONS.toml = %d, want LatestOwnerBundleVersion = %d", got, LatestOwnerBundleVersion)
		}
	})
}

// TestReservationLedgerHasNoDuplicatesOrGaps proves the ledger ITSELF is a clean
// contiguous 1..N sequence with no duplicate ordinal — the property that lets the
// match-the-dir test above conclude a missing/extra row rather than a quietly
// colliding one. (assertLedgerMatchesDir relies on this; this test states it
// directly so a regression names the precise defect.)
func TestReservationLedgerHasNoDuplicatesOrGaps(t *testing.T) {
	ledger, err := Reservations()
	if err != nil {
		t.Fatalf("parse RESERVATIONS.toml: %v", err)
	}
	assertContiguousNoDup(t, "runtime_migration", ledger.RuntimeMigrations)
	assertContiguousNoDup(t, "owner_bundle", ledger.OwnerBundles)
}

// TestParseReservationsRejectsMalformedLedgers proves the parser fails closed on
// the shapes a careless edit would introduce, so a broken ledger reds the guard
// loudly rather than silently parsing to an empty/partial set.
func TestParseReservationsRejectsMalformedLedgers(t *testing.T) {
	cases := map[string]string{
		"ordinal without file": "[[runtime_migration]]\nordinal = 5\n",
		"file without ordinal": "[[owner_bundle]]\nfile = \"0001_x.sql\"\n",
		"non-integer ordinal":  "[[runtime_migration]]\nordinal = five\nfile = \"x.sql\"\n",
		"key outside a block":  "ordinal = 1\nfile = \"x.sql\"\n",
		"unknown table header": "[[mystery]]\nordinal = 1\nfile = \"x.sql\"\n",
		"value without equals": "[[runtime_migration]]\nordinal\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseReservations(body); err == nil {
				t.Fatalf("parseReservations(%q) = nil error, want a refusal", body)
			}
		})
	}

	// A well-formed entry that also carries an optional documentation-only `note`
	// must parse cleanly (note is ignored, not an error).
	good := "[[runtime_migration]]\nordinal = 7\nfile = \"0007_x.sql\"\nnote = \"some context\"\n"
	ledger, err := parseReservations(good)
	if err != nil {
		t.Fatalf("parseReservations(well-formed with note) errored: %v", err)
	}
	if len(ledger.RuntimeMigrations) != 1 || ledger.RuntimeMigrations[0].Ordinal != 7 || ledger.RuntimeMigrations[0].File != "0007_x.sql" {
		t.Fatalf("parseReservations(well-formed) = %#v, want one entry {7, 0007_x.sql}", ledger.RuntimeMigrations)
	}
}

// ordinalsToFilesOnDisk reads dir (relative to the package directory) and returns
// the NNNN ordinal -> filename map for its leading-numeric *.sql files.
func ordinalsToFilesOnDisk(t *testing.T, dir string) map[int]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[int]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		head := strings.SplitN(name, "_", 2)[0]
		n, err := strconv.Atoi(head)
		if err != nil {
			// Not a leading-numeric migration file (e.g. RESERVATIONS.toml is not
			// even *.sql); skip it.
			continue
		}
		if prior, ok := out[n]; ok {
			t.Fatalf("two %s files share ordinal %d: %s and %s", dir, n, prior, name)
		}
		out[n] = name
	}
	return out
}

// assertLedgerMatchesDir fails on any disagreement between the ledger entries and
// the on-disk ordinal->file map: a file with no ledger row, a ledger row with no
// file, or a ledger row whose filename does not match the on-disk file.
func assertLedgerMatchesDir(t *testing.T, kind string, entries []ReservationEntry, onDisk map[int]string) {
	t.Helper()
	ledgerByOrdinal := map[int]string{}
	for _, entry := range entries {
		if prior, ok := ledgerByOrdinal[entry.Ordinal]; ok {
			t.Fatalf("RESERVATIONS.toml reserves %s ordinal %d twice (%s and %s): a duplicate ordinal", kind, entry.Ordinal, prior, entry.File)
		}
		ledgerByOrdinal[entry.Ordinal] = entry.File
	}
	for ordinal, file := range onDisk {
		ledgerFile, ok := ledgerByOrdinal[ordinal]
		if !ok {
			t.Fatalf("%s file %s is on disk but has NO RESERVATIONS.toml row; register it (ordinal=%d file=%q)", kind, file, ordinal, file)
		}
		if ledgerFile != file {
			t.Fatalf("%s ordinal %d: RESERVATIONS.toml reserves %q but the on-disk file is %q", kind, ordinal, ledgerFile, file)
		}
	}
	for ordinal, file := range ledgerByOrdinal {
		if _, ok := onDisk[ordinal]; !ok {
			t.Fatalf("RESERVATIONS.toml reserves %s ordinal %d (%q) but no such file exists on disk", kind, ordinal, file)
		}
	}
}

// assertContiguousNoDup fails if the entries are not a contiguous 1..N sequence
// (a gap or a duplicate ordinal).
func assertContiguousNoDup(t *testing.T, kind string, entries []ReservationEntry) {
	t.Helper()
	ordinals := make([]int, 0, len(entries))
	for _, entry := range entries {
		ordinals = append(ordinals, entry.Ordinal)
	}
	sort.Ints(ordinals)
	for i, ordinal := range ordinals {
		want := i + 1
		if ordinal != want {
			if i > 0 && ordinal == ordinals[i-1] {
				t.Fatalf("%s ordinal %d is reserved more than once (a duplicate)", kind, ordinal)
			}
			t.Fatalf("%s ordinals are not contiguous: expected %d at position %d, got %d (a gap or out-of-order reservation)", kind, want, i, ordinal)
		}
	}
}

func highestOrdinal(entries []ReservationEntry) int {
	highest := 0
	for _, entry := range entries {
		if entry.Ordinal > highest {
			highest = entry.Ordinal
		}
	}
	return highest
}
