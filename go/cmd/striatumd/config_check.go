package main

import (
	"fmt"
	"os"

	"github.com/halbritt/striatum/go/pkg/blob"
	"github.com/halbritt/striatum/go/pkg/db"
)

// exitConfigError is the process exit code striatumd uses for a deterministic
// configuration error — a malformed or missing config value that a restart
// cannot fix. It is BSD sysexits EX_CONFIG (78). The installed systemd unit sets
// RestartPreventExitStatus=78 so a config error parks the daemon in `failed`
// with a clear message instead of crash-looping; transient/operational failures
// keep their non-78 exit and still auto-restart.
const exitConfigError = 78

// exitAwaitingOwnerDDL is the dedicated, NON-RESTARTABLE exit status striatumd
// uses for the RFC 0142 Layer 2 owner-bundle watermark shortfall: the applied
// `owner_bundle_meta` watermark is below the frontier this binary requires, so a
// runtime migration that depends on the pending owner DDL would crash-loop the
// single writer (#442 / D248). Like the config error this is a deterministic
// condition a bare restart cannot fix — the operator must apply the pending owner
// bundle out-of-band (`striatum daemon owner-ddl apply`) first — so the installed
// unit adds this code to RestartPreventExitStatus and the daemon parks in
// `failed` with the remediation in `systemctl --user status` (apoptosis, not a
// crash loop). It is distinct from 78 so a watermark halt is legible as its own
// condition rather than masquerading as a malformed config. BSD sysexits leaves
// 79 unassigned; we use it for this daemon-specific deterministic halt.
const exitAwaitingOwnerDDL = 79

// daemonConfigProblems validates every configuration source the daemon reads at
// startup WITHOUT side effects — no database connection, no socket bind, no
// runtime reservation — and returns every problem it finds (it does not stop at
// the first, so one fix clears the whole set). It is the single validator shared
// by the `-check-config` preflight and the startup config guard, so the two
// cannot drift. Connectivity is deliberately out of scope: a parseable DSN whose
// database is momentarily down is a transient failure the daemon should retry,
// not a config error.
func daemonConfigProblems(postgresURL, pgWriteBoundary, auditHashFormat string) []error {
	var problems []error

	cfg := db.ResolveConfig(postgresURL)
	if cfg.URL == "" {
		problems = append(problems, fmt.Errorf(
			"no PostgreSQL URL configured: pass --postgres-url, set %s, or add postgres_url to %s",
			db.EnvDaemonDBURL, cfg.ConfigPath))
	} else if err := db.ValidateDSN(cfg.URL); err != nil {
		problems = append(problems, fmt.Errorf("PostgreSQL URL (from %s): %w", cfg.Source, err))
	}

	if _, err := db.ResolveWriteBoundary(pgWriteBoundary, auditHashFormat); err != nil {
		problems = append(problems, err)
	}

	if _, err := blob.LoadConfig(); err != nil && !blob.IsDisabled(err) {
		problems = append(problems, fmt.Errorf("blob storage: %w", err))
	}

	return problems
}

// runConfigCheck implements the `-check-config` preflight: it validates the
// daemon configuration and returns the process exit code (0 when valid,
// exitConfigError when not). It performs no side effects, so an operator can run
// it safely against a live deployment before restarting.
func runConfigCheck(postgresURL, pgWriteBoundary, auditHashFormat string) int {
	problems := daemonConfigProblems(postgresURL, pgWriteBoundary, auditHashFormat)
	if len(problems) == 0 {
		fmt.Println("striatumd configuration OK")
		return 0
	}
	for _, p := range problems {
		fmt.Fprintf(os.Stderr, "config error: %v\n", p)
	}
	fmt.Fprintf(os.Stderr, "striatumd configuration invalid: %d problem(s)\n", len(problems))
	return exitConfigError
}
