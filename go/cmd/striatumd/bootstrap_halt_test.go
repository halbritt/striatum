package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
)

func TestBootstrapNonRestartableRemediationClassifiesMigrationHashMismatch(t *testing.T) {
	err := &db.MigrationHashMismatchError{
		Version:  12,
		Expected: "expected-sha",
		Actual:   "actual-sha",
		Source:   "recorded",
	}

	remediation, ok := bootstrapNonRestartableRemediation(err)

	if !ok {
		t.Fatal("migration hash mismatch must map to the non-restartable bootstrap halt")
	}
	if !strings.Contains(remediation, "schema_migrations") {
		t.Fatalf("remediation = %q, want schema_migrations guidance", remediation)
	}
}

func TestBootstrapNonRestartableRemediationIgnoresGenericErrors(t *testing.T) {
	if remediation, ok := bootstrapNonRestartableRemediation(errors.New("temporary postgres dial failure")); ok {
		t.Fatalf("generic errors must not map to non-restartable halt, got %q", remediation)
	}
}
