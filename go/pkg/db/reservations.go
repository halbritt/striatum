package db

import (
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// RFC 0142 Layer 0 (D258): the reservation ledger is the committed allocation
// authority for migration / owner-bundle ordinals. It closes RFC 0142 failure
// #4 (two PRs silently minting the same NNNN — the 0039 / 0041 collisions): a
// migration is not reserved until its row exists here, and the
// TestReservationLedgerMatchesOnDisk guard reds any disagreement between the
// ledger and the on-disk *.sql set (missing row, missing file, duplicate
// ordinal, or gap). It is a normal go test in pkg/db, so CI's `go test` IS the
// collision guard.

//go:embed sql/RESERVATIONS.toml
var reservationsTOML string

// ReservationEntry is one reserved ordinal: the NNNN of a migration / bundle
// file and the filename it is reserved for.
type ReservationEntry struct {
	Ordinal int
	File    string
}

// ReservationLedger is the parsed RESERVATIONS.toml: the reserved
// runtime-migration and owner-bundle ordinals, in ascending order.
type ReservationLedger struct {
	RuntimeMigrations []ReservationEntry
	OwnerBundles      []ReservationEntry
}

// Reservations parses the embedded RESERVATIONS.toml. It uses a deliberately
// tiny array-of-tables parser (no TOML dependency) that reads only the
// `[[runtime_migration]]` / `[[owner_bundle]]` blocks and their `ordinal` and
// `file` keys; any other key (e.g. an optional `note`) is ignored, and comment
// (`#`) and blank lines are skipped. The format is kept this simple on purpose
// so the ledger stays human-legible and the parser stays auditable.
func Reservations() (ReservationLedger, error) {
	return parseReservations(reservationsTOML)
}

func parseReservations(body string) (ReservationLedger, error) {
	var ledger ReservationLedger
	// section identifies which array-of-tables block the current key/value
	// lines belong to; "" means no block is open yet.
	const (
		sectionNone    = ""
		sectionRuntime = "runtime_migration"
		sectionOwner   = "owner_bundle"
	)
	section := sectionNone
	var cur ReservationEntry
	have := false

	flush := func() error {
		if !have {
			return nil
		}
		if cur.Ordinal == 0 || cur.File == "" {
			return fmt.Errorf("RESERVATIONS.toml: incomplete %s entry (ordinal=%d file=%q): both ordinal and file are required", section, cur.Ordinal, cur.File)
		}
		switch section {
		case sectionRuntime:
			ledger.RuntimeMigrations = append(ledger.RuntimeMigrations, cur)
		case sectionOwner:
			ledger.OwnerBundles = append(ledger.OwnerBundles, cur)
		}
		cur = ReservationEntry{}
		have = false
		return nil
	}

	for lineNo, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch line {
		case "[[runtime_migration]]":
			if err := flush(); err != nil {
				return ReservationLedger{}, err
			}
			section = sectionRuntime
			have = true
			continue
		case "[[owner_bundle]]":
			if err := flush(); err != nil {
				return ReservationLedger{}, err
			}
			section = sectionOwner
			have = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			return ReservationLedger{}, fmt.Errorf("RESERVATIONS.toml line %d: unexpected table header %q (only [[runtime_migration]] and [[owner_bundle]] are recognized)", lineNo+1, line)
		}
		if section == sectionNone || !have {
			return ReservationLedger{}, fmt.Errorf("RESERVATIONS.toml line %d: key %q appears outside an array-of-tables block", lineNo+1, line)
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return ReservationLedger{}, fmt.Errorf("RESERVATIONS.toml line %d: expected `key = value`, got %q", lineNo+1, line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "ordinal":
			n, err := strconv.Atoi(value)
			if err != nil {
				return ReservationLedger{}, fmt.Errorf("RESERVATIONS.toml line %d: ordinal %q is not an integer: %w", lineNo+1, value, err)
			}
			cur.Ordinal = n
		case "file":
			cur.File = strings.Trim(value, `"`)
		default:
			// An optional documentation-only key (e.g. `note`); ignore it.
		}
	}
	if err := flush(); err != nil {
		return ReservationLedger{}, err
	}

	sort.Slice(ledger.RuntimeMigrations, func(i, j int) bool {
		return ledger.RuntimeMigrations[i].Ordinal < ledger.RuntimeMigrations[j].Ordinal
	})
	sort.Slice(ledger.OwnerBundles, func(i, j int) bool {
		return ledger.OwnerBundles[i].Ordinal < ledger.OwnerBundles[j].Ordinal
	})
	return ledger, nil
}
